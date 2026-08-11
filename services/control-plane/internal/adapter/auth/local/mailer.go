package local

import (
	"context"
	"fmt"
	"net/smtp"
)

// Mailer is the abstraction over outbound email used by the local auth adapter.
// Tests substitute a stub; production wires SMTPMailer to MailHog or a real SES.
type Mailer interface {
	SendMagicLink(ctx context.Context, to, link string) error
	// SendPasswordReset delivers the Phase 9 password-reset link. Kept as a
	// separate method (not a purpose flag on SendMagicLink) so transports can
	// use a distinct subject/template per flow.
	SendPasswordReset(ctx context.Context, to, link string) error
}

// SMTPMailer sends mail through a plain SMTP relay. Suitable for MailHog and SES
// (with TLS terminated upstream). No auth, no STARTTLS — deliberately dev-only
// until Phase 7 swaps in a hardened transport.
type SMTPMailer struct {
	Host string
	Port string
	From string
}

func (m *SMTPMailer) SendMagicLink(_ context.Context, to, link string) error {
	body := fmt.Sprintf("Subject: Your NEXIS magic link\r\nFrom: %s\r\nTo: %s\r\n\r\nClick: %s\r\n",
		m.From, to, link)
	addr := m.Host + ":" + m.Port
	return smtp.SendMail(addr, nil, m.From, []string{to}, []byte(body))
}

func (m *SMTPMailer) SendPasswordReset(_ context.Context, to, link string) error {
	body := fmt.Sprintf("Subject: Reset your NEXIS password\r\nFrom: %s\r\nTo: %s\r\n\r\nReset your password: %s\r\n\r\nIf you didn't request this, you can ignore this email.\r\n",
		m.From, to, link)
	addr := m.Host + ":" + m.Port
	return smtp.SendMail(addr, nil, m.From, []string{to}, []byte(body))
}

// TestMailer records the last link emitted instead of sending. Used by unit tests
// to retrieve the plaintext magic-link / reset token issued by the provider.
type TestMailer struct {
	lastLink string
}

func (m *TestMailer) SendMagicLink(_ context.Context, _, link string) error {
	m.lastLink = link
	return nil
}

func (m *TestMailer) SendPasswordReset(_ context.Context, _, link string) error {
	m.lastLink = link
	return nil
}

// LastLink exposes the most recent link for tests outside this package (the
// handler-level flow tests extract the plaintext token from it).
func (m *TestMailer) LastLink() string { return m.lastLink }
