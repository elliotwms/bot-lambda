package bot_lambda

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/bwmarrin/discordgo"
	"github.com/elliotwms/bot-lambda/internal/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TaskRegisterCommands is a built-in task which registers the endpoint's commands with Discord, overwriting the
// application's existing global commands. See RegisterCommands.
const TaskRegisterCommands = "register_commands"

// Task is work run by invoking the function directly with a TaskRequest, rather than in response to an interaction.
// For example, a deployment pipeline or a schedule can invoke tasks. The session comes from the endpoint's session
// provider.
type Task func(ctx context.Context, e *Endpoint, s *discordgo.Session) error

// TaskRequest is the payload to invoke the function with to run a task, e.g. {"task":"register_commands"}.
type TaskRequest struct {
	Task string `json:"task"`
}

// TaskResponse is returned when a task completes successfully.
type TaskResponse struct {
	Task string `json:"task"`
}

// WithTask registers a task which can be run with HandleTask or HandleInvocation. It replaces any existing task with
// the same name, including built-in tasks.
func (e *Endpoint) WithTask(name string, task Task) *Endpoint {
	e.tasks[name] = task

	return e
}

// HandleTask runs the task named in the request. It requires a session provider.
func (e *Endpoint) HandleTask(ctx context.Context, req *TaskRequest) (res *TaskResponse, err error) {
	ctx, span := tracing.Start(ctx, "handle task", trace.WithAttributes(attribute.String("task", req.Task)))
	defer func() { tracing.End(span, err) }()

	log := e.log.With(slog.String("task", req.Task))

	task, ok := e.tasks[req.Task]
	if !ok {
		return nil, fmt.Errorf("unknown task %q", req.Task)
	}

	if e.s == nil {
		return nil, fmt.Errorf("task %q requires a session provider", req.Task)
	}

	s, err := e.s(ctx)
	if err != nil {
		return nil, fmt.Errorf("get session from source: %w", err)
	}

	log.Info("Running task")
	if err := task(ctx, e, s); err != nil {
		return nil, fmt.Errorf("task %q: %w", req.Task, err)
	}
	log.Info("Task complete")

	return &TaskResponse{Task: req.Task}, nil
}

// HandleInvocation is a Lambda handler for functions which receive both interactions and tasks. It runs a TaskRequest
// with HandleTask, and handles any other payload as an interaction with HandleEvent (API Gateway) or HandleRequest
// (function URL).
//
// Tasks can only be run by invoking the function directly, which requires lambda:InvokeFunction. Requests to an API
// Gateway or function URL are always wrapped in an event, so their body can't be read as a TaskRequest.
func (e *Endpoint) HandleInvocation(ctx context.Context, payload json.RawMessage) (any, error) {
	var probe struct {
		Task       string `json:"task"`
		HTTPMethod string `json:"httpMethod"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	switch {
	case probe.Task != "":
		return e.HandleTask(ctx, &TaskRequest{Task: probe.Task})
	case probe.HTTPMethod != "":
		var event *events.APIGatewayProxyRequest
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("unmarshal api gateway event: %w", err)
		}
		return e.HandleEvent(ctx, event)
	default:
		var event *events.LambdaFunctionURLRequest
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("unmarshal function url request: %w", err)
		}
		return e.HandleRequest(ctx, event)
	}
}
