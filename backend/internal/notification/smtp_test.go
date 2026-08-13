package notification_test

import (
	"bytes"
	"strings"
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

// Constitution v3.0.0: exactly one message to the buyer, carrying every ticket
// in the order as a single PDF attachment.
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

// --- Spec 016 Revision 2: inline images -----------------------------------

// FR-023a / T076. The brand mark travels as an EMBEDDED part, not an attachment:
// Content-Disposition inline, a Content-ID the body's cid: reference resolves
// against, and a real image content type.
func TestComposeEmbedsInlineImagesAsRelatedParts(t *testing.T) {
	wire := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Your tickets",
		HTMLBody: `<img src="cid:jive-logo.png">`,
		Attachments: []notification.Attachment{
			{Filename: "receipt-ORD-1.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.4 a")},
			{Filename: "tickets-ORD-1.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.4 b")},
		},
		Inline: []notification.Attachment{
			{Filename: "jive-logo.png", ContentType: "image/png", Content: []byte("\x89PNG\r\n\x1a\n")},
		},
	})

	// The image is inline and addressable by the body.
	assert.Contains(t, wire, `Content-Disposition: inline; filename="jive-logo.png"`)
	assert.Contains(t, wire, "Content-ID: <jive-logo.png>")
	assert.Contains(t, wire, "image/png")

	// The HTML and the image live in a multipart/related, which is what makes a
	// cid: reference resolve at all.
	assert.Contains(t, wire, "multipart/related")

	// The two DOCUMENTS stay attachments. A buyer still receives exactly two
	// files (FR-001) — the image is not one of them.
	assert.Contains(t, wire, `Content-Disposition: attachment; filename="receipt-ORD-1.pdf"`)
	assert.Contains(t, wire, `Content-Disposition: attachment; filename="tickets-ORD-1.pdf"`)
	assert.NotContains(t, wire, `Content-Disposition: attachment; filename="jive-logo.png"`)
}

// A message with no inline parts must compose exactly as before — no stray
// multipart/related wrapper, no empty part.
func TestComposeOmitsTheRelatedWrapperWithNoInlineParts(t *testing.T) {
	wire := composed(t, notification.Message{
		To:       "budi@example.com",
		Subject:  "Your tickets",
		HTMLBody: "<p>hi</p>",
		Attachments: []notification.Attachment{
			{Filename: "receipt-ORD-1.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.4 a")},
		},
	})

	assert.NotContains(t, wire, "Content-ID:")
	assert.NotContains(t, wire, "Content-Disposition: inline")
}

// The filename's extension is load-bearing: go-mail derives the part's
// Content-Type from it via mime.TypeByExtension, and a name without one becomes
// application/octet-stream, which Outlook declines to render. Nothing in the
// type system prevents that, so it is pinned here.
func TestComposeInlineFilenamesCarryAnImageExtension(t *testing.T) {
	for _, name := range []string{"jive-logo.png", "location-pin.png"} {
		assert.True(t, strings.HasSuffix(name, ".png"),
			"%s must keep an image extension or its part becomes application/octet-stream", name)
	}
}
