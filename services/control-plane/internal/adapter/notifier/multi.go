// Package notifier implements the domain.Notifier port for Phase 6.
// Provider files (slack.go, email.go, console.go) build the per-channel
// rendering and delivery; multi.go fans out one Notification to all of them.
//
// The fanout is intentionally fire-and-forget: a Slack 5xx or an SMTP retry
// loop must NEVER fail the activity that triggered the notification. Errors
// are logged via slog at warning level and otherwise absorbed.
package notifier

import (
	"context"
	"log/slog"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Multi implements domain.Notifier by fanning out to every child notifier.
// Per-child failures are logged but never surfaced — the multi-notifier
// always returns nil so the caller (activity layer) can treat notification
// as best-effort.
type Multi struct {
	children []domain.Notifier
	logger   *slog.Logger
}

// NewMulti wires N child notifiers. Order is preserved but irrelevant — each
// child runs sequentially in the same goroutine, but a single slow Slack
// POST is bounded by the http.Client timeout inside the provider.
func NewMulti(logger *slog.Logger, children ...domain.Notifier) *Multi {
	if logger == nil {
		logger = slog.Default()
	}
	return &Multi{children: children, logger: logger}
}

// Channel returns the synthetic channel name "multi". This value is
// surfaced in audit / observability tags so an operator can tell that the
// caller wired the fanout (rather than a single channel).
func (m *Multi) Channel() string { return "multi" }

// Send dispatches the Notification to every child in order. Per-child
// errors are logged at warning level and absorbed; Send always returns nil.
func (m *Multi) Send(ctx context.Context, n domain.Notification) error {
	for _, c := range m.children {
		if err := c.Send(ctx, n); err != nil {
			m.logger.Warn("notifier.channel.failed",
				"channel", c.Channel(),
				"org_id", n.OrgID,
				"run_id", n.WorkflowRunID,
				"err", err,
			)
		}
	}
	return nil
}

// compile-time conformance check
var _ domain.Notifier = (*Multi)(nil)
