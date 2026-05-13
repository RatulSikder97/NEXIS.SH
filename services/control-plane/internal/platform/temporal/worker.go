package temporal

import (
	"context"
	"log/slog"

	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
)

// WorkerSpec holds the workflow + activity registrations passed to Start.
// Keeping a struct (rather than func args) means the workflow package can
// produce a single value carrying both registrations and the platform package
// never imports the workflow package directly — that would loop, since the
// workflow package depends on adapter which lives below platform in the arch
// graph.
//
// The slices are typed `any` so the workflow package can populate them with
// whatever types it owns; the Temporal SDK accepts interface{} for both.
type WorkerSpec struct {
	TaskQueue  string
	Workflows  []any
	Activities []any
}

// Start launches a worker on tq, registers the workflows + activities, and
// blocks until the worker shuts down. Returns the first fatal error from
// w.Run(); transient errors are surfaced via the SDK logger.
//
// Note: worker.InterruptCh() is the SDK's process-signal channel. We don't
// use ctx for cancellation here because the SDK's own Run handles
// SIGINT/SIGTERM directly — passing ctx would double up. The control-plane's
// main.go already coordinates shutdown via its own signal handler; both paths
// are equivalent and they don't deadlock each other.
func Start(_ context.Context, c client.Client, spec WorkerSpec, logger *slog.Logger) error {
	if spec.TaskQueue == "" {
		return nil
	}
	w := sdkworker.New(c, spec.TaskQueue, sdkworker.Options{
		MaxConcurrentActivityExecutionSize:     16,
		MaxConcurrentWorkflowTaskExecutionSize: 16,
	})
	for _, wf := range spec.Workflows {
		w.RegisterWorkflow(wf)
	}
	for _, ac := range spec.Activities {
		w.RegisterActivity(ac)
	}
	logger.Info("temporal worker registered",
		"task_queue", spec.TaskQueue,
		"workflows", len(spec.Workflows),
		"activities", len(spec.Activities))
	return w.Run(sdkworker.InterruptCh())
}
