package notifier

// Coverage for the email notifier. We use a fake Mailer + a fake resolver
// so the tests don't open SMTP connections.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// fakeMailer captures every SendMagicLink call so the test can assert the
// addresses and links the notifier dispatched. Satisfies local.Mailer.
type fakeMailer struct {
	mu      sync.Mutex
	sends   []sendCall
	sendErr error
}

type sendCall struct {
	Email string
	Link  string
}

func (f *fakeMailer) SendMagicLink(_ context.Context, email, link string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, sendCall{Email: email, Link: link})
	return f.sendErr
}

// SendPasswordReset keeps fakeMailer conformant with local.Mailer (Phase 9).
// The notifier never sends resets; recording keeps behaviour symmetric.
func (f *fakeMailer) SendPasswordReset(_ context.Context, email, link string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, sendCall{Email: email, Link: link})
	return f.sendErr
}

// fakeResolver returns the canned list of recipients.
type fakeResolver struct {
	recipients []Recipient
	err        error
}

func (f fakeResolver) Resolve(_ context.Context, _ string) ([]Recipient, error) {
	return f.recipients, f.err
}

// TestEmailNotifier_NewAndChannel — constructor wires everything and
// Channel() returns "email".
func TestEmailNotifier_NewAndChannel(t *testing.T) {
	e := NewEmail(&fakeMailer{}, fakeResolver{}, "https://app.example.com")
	if e.Channel() != "email" {
		t.Fatalf("channel: %q", e.Channel())
	}
}

// TestEmailNotifier_SendNoMailerNoResolver — graceful degradation: nil
// deps short-circuit without erroring.
func TestEmailNotifier_SendNoMailerNoResolver(t *testing.T) {
	e := NewEmail(nil, nil, "")
	err := e.Send(context.Background(), domain.Notification{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("nil mailer must short-circuit, got %v", err)
	}
}

// TestEmailNotifier_SendDispatchesOnePerRecipient — happy path. Three
// recipients → three SendMagicLink calls.
func TestEmailNotifier_SendDispatchesOnePerRecipient(t *testing.T) {
	m := &fakeMailer{}
	r := fakeResolver{
		recipients: []Recipient{
			{Email: "a@x.com"},
			{Email: "b@x.com"},
			{Email: "c@x.com"},
		},
	}
	e := NewEmail(m, r, "https://app.example.com")
	err := e.Send(context.Background(), domain.Notification{
		OrgID: "org-1", WorkflowRunID: "wf-1", LinkURL: "https://example/foo",
	})
	if err != nil {
		t.Fatalf("send err: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 3 {
		t.Fatalf("expected 3 sends, got %d", len(m.sends))
	}
	for _, s := range m.sends {
		if s.Link != "https://example/foo" {
			t.Fatalf("link: %q", s.Link)
		}
	}
}

// TestEmailNotifier_SendFallsBackToConsoleApprovalURL — empty LinkURL on
// the notification triggers the baseURL-derived fallback.
func TestEmailNotifier_SendFallsBackToConsoleApprovalURL(t *testing.T) {
	m := &fakeMailer{}
	r := fakeResolver{recipients: []Recipient{{Email: "a@x.com"}}}
	e := NewEmail(m, r, "https://app.example.com/")
	err := e.Send(context.Background(), domain.Notification{
		OrgID: "o", WorkflowRunID: "wf-fallback",
	})
	if err != nil {
		t.Fatalf("send err: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(m.sends))
	}
	want := "https://app.example.com/console/approvals/wf-fallback"
	if m.sends[0].Link != want {
		t.Fatalf("fallback link: %q want %q", m.sends[0].Link, want)
	}
}

// TestEmailNotifier_SendResolverErrorReturned — the resolver error wraps.
func TestEmailNotifier_SendResolverErrorReturned(t *testing.T) {
	m := &fakeMailer{}
	r := fakeResolver{err: errors.New("db down")}
	e := NewEmail(m, r, "")
	err := e.Send(context.Background(), domain.Notification{OrgID: "x"})
	if err == nil {
		t.Fatalf("expected resolver error wrap")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 0 {
		t.Fatalf("must not dispatch after resolver fail")
	}
}

// TestEmailNotifier_SendNoRecipientsIsNoOp — empty recipients short-circuit.
func TestEmailNotifier_SendNoRecipientsIsNoOp(t *testing.T) {
	m := &fakeMailer{}
	r := fakeResolver{recipients: []Recipient{}}
	e := NewEmail(m, r, "")
	err := e.Send(context.Background(), domain.Notification{OrgID: "x"})
	if err != nil {
		t.Fatalf("send err: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 0 {
		t.Fatalf("must not dispatch with no recipients")
	}
}

// TestEmailNotifier_SendSkipsBlankAddresses — blank entries in the recipient
// list are silently skipped (no SendMagicLink call), but other addresses
// still go through.
func TestEmailNotifier_SendSkipsBlankAddresses(t *testing.T) {
	m := &fakeMailer{}
	r := fakeResolver{
		recipients: []Recipient{
			{Email: ""},
			{Email: "valid@x.com"},
			{Email: ""},
		},
	}
	e := NewEmail(m, r, "")
	err := e.Send(context.Background(), domain.Notification{
		OrgID: "x", LinkURL: "https://foo",
	})
	if err != nil {
		t.Fatalf("send err: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 1 {
		t.Fatalf("expected 1 send (blanks skipped), got %d", len(m.sends))
	}
}

// TestEmailNotifier_SendFirstErrorReturned — the first mailer error is
// surfaced; subsequent recipients are still attempted (so a typo doesn't
// silence the fleet).
func TestEmailNotifier_SendFirstErrorReturned(t *testing.T) {
	m := &fakeMailer{sendErr: errors.New("smtp timeout")}
	r := fakeResolver{
		recipients: []Recipient{
			{Email: "a@x.com"},
			{Email: "b@x.com"},
		},
	}
	e := NewEmail(m, r, "")
	err := e.Send(context.Background(), domain.Notification{
		OrgID: "x", LinkURL: "https://foo",
	})
	if err == nil {
		t.Fatalf("expected first error")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sends) != 2 {
		t.Fatalf("expected all sends attempted, got %d", len(m.sends))
	}
}

// Compile-time check the fakeMailer satisfies the production mailer port.
var _ local.Mailer = (*fakeMailer)(nil)
