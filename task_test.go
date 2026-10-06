package bot_lambda

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/fakediscord/pkg/fakediscord"
	"github.com/neilotoole/slogt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopHandler(context.Context, *discordgo.Session, *discordgo.InteractionCreate, discordgo.ApplicationCommandInteractionData) error {
	return nil
}

func testSession(t *testing.T) *discordgo.Session {
	s, err := discordgo.New("Bot token")
	require.NoError(t, err)

	return s
}

func TestCommands(t *testing.T) {
	permissions := int64(discordgo.PermissionManageMessages)
	e := New(nil).
		WithMessageApplicationCommand("Foo", noopHandler).
		WithCommand(&discordgo.ApplicationCommand{Name: "bar", Type: discordgo.ChatApplicationCommand, Description: "Bar", DefaultMemberPermissions: &permissions}, noopHandler)

	commands := e.Commands()

	require.Len(t, commands, 2)
	assert.Equal(t, &discordgo.ApplicationCommand{Name: "Foo", Type: discordgo.MessageApplicationCommand}, commands[0])
	assert.Equal(t, "Bar", commands[1].Description)
	assert.Equal(t, &permissions, commands[1].DefaultMemberPermissions)
}

// fakeApplicationServer serves the endpoints used to register commands, recording the commands which were registered
func fakeApplicationServer(t *testing.T) *[]*discordgo.ApplicationCommand {
	var registered []*discordgo.ApplicationCommand

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v9/applications/@me":
			_ = json.NewEncoder(w).Encode(Application{ID: "app_id", ApproximateGuildCount: 5})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v9/applications/app_id/commands":
			body, _ := io.ReadAll(r.Body)
			assert.NoError(t, json.Unmarshal(body, &registered))
			_, _ = w.Write(body)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	fakediscord.Configure(server.URL + "/")

	return &registered
}

func TestCurrentApplication(t *testing.T) {
	fakeApplicationServer(t)

	app, err := CurrentApplication(context.Background(), testSession(t))

	require.NoError(t, err)
	assert.Equal(t, "app_id", app.ID)
	assert.Equal(t, 5, app.ApproximateGuildCount)
}

func TestRegisterCommands(t *testing.T) {
	registered := fakeApplicationServer(t)
	commands := []*discordgo.ApplicationCommand{{Name: "Foo", Type: discordgo.MessageApplicationCommand}}

	err := RegisterCommands(context.Background(), testSession(t), commands)

	require.NoError(t, err)
	assert.Equal(t, commands, *registered)
}

func TestRegisterCommands_NoCommands(t *testing.T) {
	registered := fakeApplicationServer(t)

	err := RegisterCommands(context.Background(), testSession(t), nil)

	require.NoError(t, err)
	assert.NotNil(t, *registered)
	assert.Empty(t, *registered)
}

func TestHandleInvocation_RegisterCommandsTask(t *testing.T) {
	registered := fakeApplicationServer(t)
	e := New(nil, WithLogger(slogt.New(t))).
		WithSession(testSession(t)).
		WithMessageApplicationCommand("Foo", noopHandler)

	res, err := e.HandleInvocation(context.Background(), json.RawMessage(`{"task":"register_commands"}`))

	require.NoError(t, err)
	assert.Equal(t, &TaskResponse{Task: TaskRegisterCommands}, res)
	assert.Equal(t, e.Commands(), *registered)
}

func TestHandleInvocation_CustomTask(t *testing.T) {
	s := testSession(t)
	var got *discordgo.Session
	e := New(nil, WithLogger(slogt.New(t))).
		WithSession(s).
		WithTask("foo", func(_ context.Context, _ *Endpoint, s *discordgo.Session) error {
			got = s
			return nil
		})

	res, err := e.HandleInvocation(context.Background(), json.RawMessage(`{"task":"foo"}`))

	require.NoError(t, err)
	assert.Equal(t, &TaskResponse{Task: "foo"}, res)
	assert.Same(t, s, got)
}

func TestHandleInvocation_TaskErrors(t *testing.T) {
	failing := func(context.Context, *Endpoint, *discordgo.Session) error { return errors.New("boom") }

	testCases := []struct {
		name     string
		endpoint *Endpoint
		task     string
		expected string
	}{
		{
			name:     "unknown task",
			endpoint: New(nil).WithSession(testSession(t)),
			task:     "foo",
			expected: `unknown task "foo"`,
		},
		{
			name:     "no session provider",
			endpoint: New(nil).WithTask("foo", failing),
			task:     "foo",
			expected: `task "foo" requires a session provider`,
		},
		{
			name:     "task fails",
			endpoint: New(nil).WithSession(testSession(t)).WithTask("foo", failing),
			task:     "foo",
			expected: `task "foo": boom`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.endpoint.HandleInvocation(context.Background(), json.RawMessage(`{"task":"`+tc.task+`"}`))

			assert.EqualError(t, err, tc.expected)
		})
	}
}

func TestHandleInvocation_Interactions(t *testing.T) {
	ping, err := json.Marshal(&discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionPing}})
	require.NoError(t, err)

	// an empty public key skips verification
	e := New(nil, WithLogger(slogt.New(t)))

	t.Run("function url request", func(t *testing.T) {
		payload, err := json.Marshal(events.LambdaFunctionURLRequest{
			RequestContext: events.LambdaFunctionURLRequestContext{
				HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodPost},
			},
			Body: string(ping),
		})
		require.NoError(t, err)

		res, err := e.HandleInvocation(context.Background(), payload)

		require.NoError(t, err)
		require.IsType(t, &events.LambdaFunctionURLResponse{}, res)
		assert.Equal(t, http.StatusOK, res.(*events.LambdaFunctionURLResponse).StatusCode)
	})

	t.Run("api gateway event", func(t *testing.T) {
		payload, err := json.Marshal(events.APIGatewayProxyRequest{
			HTTPMethod:     http.MethodPost,
			RequestContext: events.APIGatewayProxyRequestContext{HTTPMethod: http.MethodPost},
			Body:           string(ping),
		})
		require.NoError(t, err)

		res, err := e.HandleInvocation(context.Background(), payload)

		require.NoError(t, err)
		require.IsType(t, &events.APIGatewayProxyResponse{}, res)
		assert.Equal(t, http.StatusOK, res.(*events.APIGatewayProxyResponse).StatusCode)
	})
}
