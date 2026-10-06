package bot_lambda

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpans records the spans created during the test with the global tracer provider
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return recorder
}

func TestTracing_Request(t *testing.T) {
	recorder := recordSpans(t)
	ping, err := json.Marshal(&discordgo.InteractionCreate{Interaction: &discordgo.Interaction{Type: discordgo.InteractionPing}})
	require.NoError(t, err)

	_, err = New(nil).HandleRequest(context.Background(), &events.LambdaFunctionURLRequest{
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodPost},
		},
		Body: string(ping),
	})
	require.NoError(t, err)

	var names []string
	for _, s := range recorder.Ended() {
		names = append(names, s.Name())
	}
	assert.ElementsMatch(t, []string{"handle request", "handle", "verify", "handle interaction"}, names)
}

func TestTracing_TaskError(t *testing.T) {
	recorder := recordSpans(t)
	e := New(nil).
		WithSession(testSession(t)).
		WithTask("foo", func(context.Context, *Endpoint, *discordgo.Session) error { return errors.New("boom") })

	_, err := e.HandleTask(context.Background(), &TaskRequest{Task: "foo"})
	require.Error(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "handle task", spans[0].Name())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}
