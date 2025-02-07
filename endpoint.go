package bot_lambda

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

// HandleEvent is the lambda handler for events.APIGatewayProxyRequest (when the lambda function is integrated with API
// Gateway.
// See https://docs.aws.amazon.com/apigateway/latest/developerguide/set-up-lambda-proxy-integrations.html for more info.
func (e *Endpoint) HandleEvent(ctx context.Context, event *events.APIGatewayProxyRequest) (res *events.APIGatewayProxyResponse, err error) {
	ctx, s := xray.BeginSubsegment(ctx, "handle event")
	defer s.Close(err)

	if event.RequestContext.HTTPMethod != http.MethodPost {
		// Receiving anything other than a POST requests points to a configuration issue and should be investigated
		e.log.Error("Unexpected http method", slog.String("method", event.RequestContext.HTTPMethod))
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusMethodNotAllowed}, nil
	}

	e.log.Debug("Received event")

	body, code, err := e.handle(ctx, event.Headers, []byte(event.Body))

	if err != nil {
		return nil, err
	}

	return &events.APIGatewayProxyResponse{
		StatusCode: code,
		Body:       body,
	}, nil
}

// HandleRequest handles the events.LambdaFunctionURLRequest.
// It should be registered to the Lambda Start in a function which is configured as a single-url function.
// See https://docs.aws.amazon.com/lambda/latest/dg/urls-configuration.html for more info.
func (e *Endpoint) HandleRequest(ctx context.Context, event *events.LambdaFunctionURLRequest) (res *events.LambdaFunctionURLResponse, err error) {
	ctx, s := xray.BeginSubsegment(ctx, "handle request")
	defer s.Close(err)

	if event.RequestContext.HTTP.Method != http.MethodPost {
		// Receiving anything other than a POST requests points to a configuration issue and should be investigated
		e.log.Error("Unexpected http method", slog.String("method", event.RequestContext.HTTP.Method))
		return &events.LambdaFunctionURLResponse{StatusCode: http.StatusMethodNotAllowed}, nil
	}

	e.log.Debug(
		"Received request",
		slog.String("user_agent", event.RequestContext.HTTP.UserAgent),
	)

	body, code, err := e.handle(ctx, event.Headers, []byte(event.Body))

	if err != nil {
		return nil, err
	}

	return &events.LambdaFunctionURLResponse{
		StatusCode: code,
		Body:       body,
	}, nil
}

func (e *Endpoint) handle(ctx context.Context, headers map[string]string, body []byte) (res string, code int, err error) {
	ctx, s := xray.BeginSubsegment(ctx, "handle")
	defer s.Close(err)

	if err = e.verify(ctx, headers, body); err != nil {
		e.log.Error("Failed to verify signature", "error", err)
		return "", http.StatusUnauthorized, nil
	}

	var i *discordgo.InteractionCreate
	if err = json.Unmarshal(body, &i); err != nil {
		return "", 0, fmt.Errorf("unmarshal interaction create: %w", err)
	}

	response, err := e.handleInteraction(ctx, i)
	if err != nil {
		return "", 0, err
	}

	// if no response is provided then return a 202
	//https://discord.com/developers/docs/interactions/receiving-and-responding#interaction-callback
	if response == nil {
		return "", http.StatusAccepted, nil
	}

	bs, err := json.Marshal(response)
	if err != nil {
		return "", 0, fmt.Errorf("marshal interaction response: %w", err)
	}

	return string(bs), http.StatusOK, err
}

// verify verifies the request using the ed25519 signature as per Discord's documentation.
// See https://discord.com/developers/docs/events/webhook-events#setting-up-an-endpoint-validating-security-request-headers.
func (e *Endpoint) verify(ctx context.Context, headers map[string]string, body []byte) error {
	_, s := xray.BeginSubsegment(ctx, "verify")
	defer s.Close(nil)

	// if no public key is provided then skip verification
	if len(e.publicKey) == 0 {
		return nil
	}

	parsed := make(http.Header, len(headers))
	for k, v := range headers {
		parsed.Add(k, v)
	}

	signature := parsed.Get(headerSignature)
	if signature == "" {
		return errors.New("missing header X-Signature-Ed25519")
	}
	ts := parsed.Get(headerTimestamp)
	if ts == "" {
		return errors.New("missing header X-Signature-Timestamp")
	}

	sig, err := hex.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	verify := append([]byte(ts), body...)

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
