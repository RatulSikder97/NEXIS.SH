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

// testMailer records the last link emitted instead of sending. Used by unit tests
// to retrieve the plaintext magic-link token issued by the provider.
type testMailer struct {
	lastLink string
}

func (m *testMailer) SendMagicLink(_ context.Context, _, link string) error {
	m.lastLink = link
	return nil
}
