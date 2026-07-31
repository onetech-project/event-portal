package notification

import (
	"bytes"
	"fmt"
	"io"

	mail "github.com/go-mail/mail/v2"
)

// Attachment is a file carried by an outgoing email.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// Message is a provider-agnostic outgoing email.
type Message struct {
	To          string
	Subject     string
	HTMLBody    string
	Attachments []Attachment
}

// Mailer sends email. The service depends on this interface rather than on SMTP
// directly, so delivery can be faked in tests without a mail server.
type Mailer interface {
	Send(msg Message) error
}

// SMTPConfig describes the outgoing mail server.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

// SMTPMailer delivers mail over SMTP.
type SMTPMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer builds the SMTP mailer.
func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer {
	return &SMTPMailer{cfg: cfg}
}

// Compose builds the MIME message. It is exported so the exact wire format —
// headers, HTML part, and PDF attachment — can be asserted without a mail server.
func (m *SMTPMailer) Compose(msg Message) *mail.Message {
	out := mail.NewMessage()
	out.SetAddressHeader("From", m.cfg.From, m.cfg.FromName)
	out.SetHeader("To", msg.To)
	out.SetHeader("Subject", msg.Subject)
	out.SetBody("text/html", msg.HTMLBody)

	for _, attachment := range msg.Attachments {
		content := attachment.Content
		out.AttachReader(attachment.Filename, bytes.NewReader(content),
			mail.SetHeader(map[string][]string{
				"Content-Type": {attachment.ContentType},
			}),
			mail.Rename(attachment.Filename),
			mail.SetCopyFunc(func(w io.Writer) error {
				_, err := w.Write(content)
				return err
			}),
		)
	}
	return out
}

// Send delivers the message. An error here is meaningful: the caller must not set
// orders.email_sent unless this returns nil (Constitution Principle IV).
func (m *SMTPMailer) Send(msg Message) error {
	dialer := mail.NewDialer(m.cfg.Host, m.cfg.Port, m.cfg.Username, m.cfg.Password)
	if err := dialer.DialAndSend(m.Compose(msg)); err != nil {
		return fmt.Errorf("send email to %s: %w", msg.To, err)
	}
	return nil
}
