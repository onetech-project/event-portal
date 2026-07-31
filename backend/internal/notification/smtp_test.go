package notification_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
)

func newMailer() *notification.SMTPMailer {
	return notification.NewSMTPMailer(notification.SMTPConfig{
		Host:     "localhost",
		Port:     1025,
		From:     "tickets@example.com",
		FromName: "Event Ticketing",
	})
}

func composed(t *testing.T, msg notification.Message) string {
	t.Helper()
	var buf bytes.Buffer
	_, err := newMailer().Compose(msg).WriteTo(&buf)
	require.NoError(t, err)
	return buf.String()
}

func TestComposeSetsTheEnvelopeHeaders(t *testing.T) {
	raw := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Your tickets for Jazz Night",
		HTMLBody: "<p>Enjoy the show</p>",
	})

	assert.Contains(t, raw, "To: budi@example.com")
	assert.Contains(t, raw, "Jazz Night")
	assert.Contains(t, raw, "tickets@example.com")
}

func TestComposeSendsAnHTMLBody(t *testing.T) {
	raw := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Tickets",
		HTMLBody: "<p>Enjoy the show</p>",
	})

	assert.Contains(t, raw, "text/html")
}

func TestComposeAttachesThePDF(t *testing.T) {
	raw := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Tickets",
		HTMLBody: "<p>Enjoy</p>",
		Attachments: []notification.Attachment{{
			Filename:    "tickets-ORD-20260731-ABCDEF.pdf",
			ContentType: "application/pdf",
			Content:     []byte("%PDF-1.4 fake"),
		}},
	})

	assert.Contains(t, raw, "application/pdf")
	assert.Contains(t, raw, "tickets-ORD-20260731-ABCDEF.pdf")
}

// Exactly one email per paid order, carrying all of its tickets in one PDF
// (constitution, Critical Data Flow Rules).
func TestComposeCarriesEveryTicketInASingleAttachment(t *testing.T) {
	raw := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Tickets",
		HTMLBody: "<p>Enjoy</p>",
		Attachments: []notification.Attachment{{
			Filename:    "tickets.pdf",
			ContentType: "application/pdf",
			Content:     bytes.Repeat([]byte("A"), 2048),
		}},
	})

	assert.Equal(t, 1, bytes.Count([]byte(raw), []byte("application/pdf")))
}

func TestSendReportsAnUnreachableServer(t *testing.T) {
	mailer := notification.NewSMTPMailer(notification.SMTPConfig{
		// Port 1 is reserved and never accepts SMTP, so this fails fast.
		Host: "127.0.0.1", Port: 1, From: "tickets@example.com",
	})

	err := mailer.Send(notification.Message{
		To: "budi@example.com", Subject: "Tickets", HTMLBody: "<p>Hi</p>",
	})

	require.Error(t, err, "a failed delivery must surface so email_sent is not set")
}
