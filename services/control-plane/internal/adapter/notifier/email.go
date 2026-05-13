package notifier

import (
	"context"
	"fmt"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Recipient is the email destination the resolver returns. Subject + body
// rendering does not vary per recipient — the same content lands in each
// recipient's inbox.
type Recipient struct {
	Email string
	Name  string
}

// RecipientResolver returns the list of admin/owner emails for an org. The
// concrete implementation typically wraps a WorkspaceMembersRepo so the
// notifier doesn't take a pgx dependency directly. Resolver errors are
// logged + absorbed — a notifier that can't find recipients still fails
// soft so the activity proceeds.
type RecipientResolver interface {
	Resolve(ctx context.Context, orgID string) ([]Recipient, error)
}

// LinkRenderer reuses the same template the Slack notifier builds. Lifted
// to a named type so tests can swap in a deterministic value.
type LinkRenderer interface {
	LinkURL(n domain.Notification) string
}

// Email is the domain.Notifier implementation that fans out one
// Notification to every workspace admin/owner via the existing SMTP
// mailer. The Phase 2 mailer's SendMagicLink method is reused — the body
// shape is the same plaintext "Click: <link>" we already ship.
type Email struct {
	mailer    local.Mailer
	resolver  RecipientResolver
	link      LinkRenderer
	from      string
	baseURL   string
}

// NewEmail builds an Email notifier. mailer is typically *local.SMTPMailer
// (production) or *local.TestMailer (tests). When resolver is nil Send is a
// no-op — the notifier degrades gracefully rather than failing the
// activity when the recipients lookup isn't wired yet.
func NewEmail(mailer local.Mailer, resolver RecipientResolver, baseURL string) *Email {
	return &Email{mailer: mailer, resolver: resolver, baseURL: baseURL}
}

// Channel returns the canonical channel name "email".
func (e *Email) Channel() string { return "email" }

// Send resolves recipients then dispatches one SendMagicLink call per
// address. We reuse SendMagicLink rather than introducing a new mailer
// method — the wire shape is the same: subject + plaintext body + URL.
// Per-recipient failures are absorbed (returning the first error to the
// multi-fanout layer); the loop continues so a single bad address doesn't
// silence the rest of the fleet.
func (e *Email) Send(ctx context.Context, n domain.Notification) error {
	if e.mailer == nil || e.resolver == nil {
		return nil
	}
	recipients, err := e.resolver.Resolve(ctx, n.OrgID)
	if err != nil {
		return fmt.Errorf("email notifier: resolve recipients: %w", err)
	}
	if len(recipients) == 0 {
		return nil
	}

	link := n.LinkURL
	if link == "" {
		link = trimRight(e.baseURL, '/') + "/console/approvals/" + n.WorkflowRunID
	}

	var firstErr error
	for _, r := range recipients {
		if r.Email == "" {
			continue
		}
		if err := e.mailer.SendMagicLink(ctx, r.Email, link); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// compile-time conformance check
var _ domain.Notifier = (*Email)(nil)
