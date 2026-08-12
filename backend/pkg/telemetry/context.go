package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const (
	taskIDKey         contextKey = "nemesiscode.task.id"
	agentSessionIDKey contextKey = "nemesiscode.agent.session.id"
	businessReqIDKey  contextKey = "nemesiscode.request.id"
	projectIDKey      contextKey = "nemesiscode.project.id"
	vmIDKey           contextKey = "taskflow.vm.id"
	terminalIDKey     contextKey = "taskflow.terminal.session.id"
)

func WithTaskID(ctx context.Context, id string) context.Context {
	return withID(ctx, taskIDKey, "nemesiscode.task.id", id)
}

func WithAgentSessionID(ctx context.Context, id string) context.Context {
	return withID(ctx, agentSessionIDKey, "nemesiscode.agent.session.id", id)
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return withID(ctx, businessReqIDKey, "nemesiscode.request.id", id)
}

func WithProjectID(ctx context.Context, id string) context.Context {
	return withID(ctx, projectIDKey, "nemesiscode.project.id", id)
}

func WithVMID(ctx context.Context, id string) context.Context {
	return withID(ctx, vmIDKey, "taskflow.vm.id", id)
}

func WithTerminalSessionID(ctx context.Context, id string) context.Context {
	return withID(ctx, terminalIDKey, "taskflow.terminal.session.id", id)
}

func MarkCritical(ctx context.Context) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("telemetry.priority", "critical"))
}

func SetOutcome(ctx context.Context, outcome string) {
	if outcome != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("task.outcome", outcome))
	}
}

func withID(ctx context.Context, key contextKey, name, id string) context.Context {
	if id == "" {
		return ctx
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String(name, id))
	return context.WithValue(ctx, key, id)
}

func LogAttrs(ctx context.Context) []slog.Attr {
	attrs := make([]slog.Attr, 0, 8)
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	for _, item := range []struct {
		key  contextKey
		name string
	}{
		{taskIDKey, "nemesiscode.task.id"},
		{agentSessionIDKey, "nemesiscode.agent.session.id"},
		{businessReqIDKey, "nemesiscode.request.id"},
		{projectIDKey, "nemesiscode.project.id"},
		{vmIDKey, "taskflow.vm.id"},
		{terminalIDKey, "taskflow.terminal.session.id"},
	} {
		if value, ok := ctx.Value(item.key).(string); ok && value != "" {
			attrs = append(attrs, slog.String(item.name, value))
		}
	}
	return attrs
}
