package bot_lambda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot/interactions/router"
	"github.com/elliotwms/fakediscord/pkg/fakediscord"
	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpoint_ApplicationCommand(t *testing.T) {
	// given an endpoint
	l := slogt.New(t)
	r := router.New(router.WithLogger(l))
	e := New(nil, WithLogger(l), WithRouter(r))

	// given the endpoint has command foo
	calls := 0
	e.WithMessageApplicationCommand("foo", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) (err error) {
		calls++
		return nil
	})

	// given an interaction
	body, err := json.Marshal(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:  discordgo.InteractionApplicationCommand,
			Token: "interaction_token",
			Data: discordgo.ApplicationCommandInteractionData{
				Name:        "foo",
				CommandType: discordgo.MessageApplicationCommand,
			},
		},
	})
	require.NoError(t, err)

	// when the endpoint receives the interaction
	res, err := e.HandleRequest(context.Background(), &events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodPost},
		},
		Body: string(body),
	})

	// then the interaction should be responded to successfully
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, http.StatusAccepted, res.StatusCode)

	// then the handler should have been called n times
	assert.Equal(t, 1, calls)
}

func TestEndpoint_ApplicationCommandWithDeferredResponse(t *testing.T) {
	// given an endpoint
	l := slogt.New(t)
	r := router.New(router.WithLogger(l))
	e := New(
		nil,
		WithLogger(l),
		WithRouter(r),
		WithDeferredResponseEnabled(true),
	)

	// given the endpoint has application command foo
	calls := 0
	e.WithMessageApplicationCommand("foo", func(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) (err error) {
		calls++
		return nil
	})

	// an interaction
	body, err := json.Marshal(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:    "interaction_id",
			Type:  discordgo.InteractionApplicationCommand,
			Token: "interaction_token",
			Data: discordgo.ApplicationCommandInteractionData{
				Name:        "foo",
				CommandType: discordgo.MessageApplicationCommand,
			},
		},
	})
	require.NoError(t, err)

	// the interaction response endpoint expects a request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "/api/v9/interactions/interaction_id/interaction_token/callback", r.URL.String())
	}))
	t.Cleanup(server.Close)
	fakediscord.Configure(server.URL + "/")

	// when the endpoint receives the interaction
	res, err := e.HandleRequest(context.Background(), &events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodPost},
		},
		Body: string(body),
	})

	// then there should be no error
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, http.StatusAccepted, res.StatusCode)

	// then the handler should have been called n times
	assert.Equal(t, 1, calls)
}
