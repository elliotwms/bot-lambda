package lambda

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-xray-sdk-go/xray"
	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot-lambda/sessionprovider"
	"github.com/elliotwms/bot/interactions/router"
	"github.com/elliotwms/bot/log"
)

const (
	headerSignature = "X-Signature-Ed25519"
	headerTimestamp = "X-Signature-Timestamp"
)

type Endpoint struct {
	s         sessionprovider.Provider
	publicKey ed25519.PublicKey
	router    *router.Router
	log       *slog.Logger
}

func New(publicKey ed25519.PublicKey, options ...Option) *Endpoint {
	logger := slog.New(log.DiscardHandler)

	e := &Endpoint{
		publicKey: publicKey,
		log:       logger,
		router:    router.New(router.WithLogger(logger)),
	}

	for _, o := range options {
		o(e)
	}

	return e
}

type Option func(*Endpoint)

// WithRouter overrides the underlying router used for the endpoint.
func WithRouter(router *router.Router) Option {
	return func(endpoint *Endpoint) {
		endpoint.router = router
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(endpoint *Endpoint) {
		endpoint.log = logger
	}
}

// WithSessionProvider adds a provider which will be called before each handler invocation to override the interaction's
// default session (created using the interaction's token).
// This is useful in scenarios where the bot requires more permissions than is provided by the token provided by the
// interaction.
func (e *Endpoint) WithSessionProvider(f sessionprovider.Provider) *Endpoint {
	e.s = f

	return e
}

// WithSession adds a hardcoded global session. See WithSessionProvider for more info.
func (e *Endpoint) WithSession(s *discordgo.Session) *Endpoint {
	e.s = sessionprovider.Static(s)

	return e
}

// WithChatApplicationCommand registers a new discordgo.ChatApplicationCommand.
// This is syntactic sugar for WithApplicationCommand
func (e *Endpoint) WithChatApplicationCommand(name string, handler router.ApplicationCommandHandler) *Endpoint {
	return e.WithApplicationCommand(name, discordgo.ChatApplicationCommand, handler)
}

// WithUserApplicationCommand registers a new discordgo.UserApplicationCommand.
// This is syntactic sugar for WithApplicationCommand
func (e *Endpoint) WithUserApplicationCommand(name string, handler router.ApplicationCommandHandler) *Endpoint {
	return e.WithApplicationCommand(name, discordgo.UserApplicationCommand, handler)
}

// WithMessageApplicationCommand registers a new discordgo.MessageApplicationCommand.
// This is syntactic sugar for WithApplicationCommand
func (e *Endpoint) WithMessageApplicationCommand(name string, handler router.ApplicationCommandHandler) *Endpoint {
	return e.WithApplicationCommand(name, discordgo.MessageApplicationCommand, handler)
}

// WithApplicationCommand registers a new application command with the underlying Router.
func (e *Endpoint) WithApplicationCommand(name string, commandType discordgo.ApplicationCommandType, handler router.ApplicationCommandHandler) *Endpoint {
	e.router.RegisterCommand(name, commandType, handler)

	return e
}

// Handle handles the events.LambdaFunctionURLRequest.
// It should be registered to the Lambda Start in a function which is configured as a single-url function.
// See https://docs.aws.amazon.com/lambda/latest/dg/urls-configuration.html for more info.
func (e *Endpoint) Handle(ctx context.Context, event *events.LambdaFunctionURLRequest) (res *events.LambdaFunctionURLResponse, err error) {
	ctx, s := xray.BeginSubsegment(ctx, "handle")
	defer s.Close(err)
	if event == nil {
		return nil, fmt.Errorf("received nil event")
	}

	bs := []byte(event.Body)

	e.log.Debug(
		"Received request",
		slog.String("user_agent", event.RequestContext.HTTP.UserAgent),
		slog.String("method", event.RequestContext.HTTP.Method),
	)

	if err = e.verify(ctx, event); err != nil {
		e.log.Error("Failed to verify signature", "error", err)
		return &events.LambdaFunctionURLResponse{
			StatusCode: http.StatusUnauthorized,
		}, nil
	}

	var i *discordgo.InteractionCreate
	if err = json.Unmarshal(bs, &i); err != nil {
		return nil, err
	}

	response, err := e.handleInteraction(ctx, i)
	if err != nil {
		return nil, err
	}

	if response == nil {
		return &events.LambdaFunctionURLResponse{StatusCode: http.StatusAccepted}, nil
	}

	bs, err = json.Marshal(response)
	if err != nil {
		return nil, err
	}

	return &events.LambdaFunctionURLResponse{
		StatusCode: http.StatusOK,
		Body:       string(bs),
	}, nil
}

// verify verifies the request using the ed25519 signature as per Discord's documentation.
// See https://discord.com/developers/docs/events/webhook-events#setting-up-an-endpoint-validating-security-request-headers.
func (e *Endpoint) verify(ctx context.Context, event *events.LambdaFunctionURLRequest) error {
	_, s := xray.BeginSubsegment(ctx, "verify")
	defer s.Close(nil)

	if len(e.publicKey) == 0 {
		return nil
	}

	headers := make(http.Header, len(event.Headers))
	for k, v := range event.Headers {
		headers.Add(k, v)
	}

	signature := headers.Get(headerSignature)
	if signature == "" {
		return errors.New("missing header X-Signature-Ed25519")
	}
	ts := headers.Get(headerTimestamp)
	if ts == "" {
		return errors.New("missing header X-Signature-Timestamp")
	}

	sig, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	verify := append([]byte(ts), []byte(event.Body)...)

	if !ed25519.Verify(e.publicKey, verify, sig) {
		return errors.New("invalid signature")
	}

	return nil
}

// handleInteraction handles the discordgo.InteractionCreate, returning an optional sync response
func (e *Endpoint) handleInteraction(ctx context.Context, i *discordgo.InteractionCreate) (*discordgo.InteractionResponse, error) {
	e.log.Debug("Handling interaction", "type", i.Type, "interaction_id", i.ID)
	ctx, seg := xray.BeginSubsegment(ctx, "handle interaction")
	_ = seg.AddAnnotation("type", int(i.Type))
	defer seg.Close(nil)

	var s *discordgo.Session

	// if a session provided exists then use it as the session source
	if e.s != nil {
		var err error
		s, err = e.s(ctx)
		if err != nil {
			return nil, fmt.Errorf("get session from source: %w", err)
		}
	} else {
		// otherwise build a session scoped for the interaction
		s, _ = discordgo.New("Bot " + i.Token)
		s.Client = xray.Client(s.Client)
	}

	return e.router.HandleWithContext(ctx, s, i), nil
}
