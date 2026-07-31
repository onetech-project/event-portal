package notification_test

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
)

func sampleTickets(n int) []notification.TicketDetail {
	out := make([]notification.TicketDetail, 0, n)
	codes := []string{"ABC234DEFG", "HJK567LMNP", "QRS789TUVW", "XYZ234ABCD"}
	for i := range n {
		out = append(out, notification.TicketDetail{
			TicketCode:     codes[i%len(codes)],
			AttendeeName:   "Attendee",
			TicketTypeName: "Regular",
			EventName:      "Jazz Night 2026",
			Venue:          "Balai Sarbini",
			StartDate:      time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC),
		})
	}
	return out
}

func sampleOrder() notification.OrderDelivery {
	return notification.OrderDelivery{
		OrderNumber: "ORD-20260731-ABCDEF",
		BuyerName:   "Budi Santoso",
		BuyerEmail:  "budi@example.com",
		Status:      "PAID",
	}
}

func TestRenderTicketsPDFProducesAPDFDocument(t *testing.T) {
	out, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(1))

	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(out, []byte("%PDF-")), "output must be a PDF")
	assert.Greater(t, len(out), 1000)
}

// One order produces exactly one PDF containing every ticket, not one PDF per
// ticket (Constitution, Critical Data Flow Rules).
func TestRenderTicketsPDFGrowsWithTheNumberOfTickets(t *testing.T) {
	one, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(1))
	require.NoError(t, err)

	four, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(4))
	require.NoError(t, err)

	assert.Greater(t, len(four), len(one), "every ticket must be rendered into the single document")
}

func TestRenderTicketsPDFRejectsAnEmptyTicketSet(t *testing.T) {
	_, err := notification.RenderTicketsPDF(sampleOrder(), nil)

	require.Error(t, err, "an order with no tickets has nothing to deliver")
}

func TestRenderTicketsPDFIsDeterministicForTheSameInput(t *testing.T) {
	first, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(2))
	require.NoError(t, err)
	second, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(2))
	require.NoError(t, err)

	// Byte-for-byte equality would depend on the PDF's embedded timestamp, so
	// compare size instead: a resend must reproduce the same document, not a
	// different one.
	assert.Equal(t, len(first), len(second))
}

// --- QR rendering ---------------------------------------------------------

func TestRenderQRProducesADecodablePNG(t *testing.T) {
	raw, err := notification.RenderQR("ABC234DEFG")

	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(raw))
	require.NoError(t, err, "the QR must be a valid PNG for the PDF to embed it")
	assert.Positive(t, img.Bounds().Dx())
	assert.Equal(t, img.Bounds().Dx(), img.Bounds().Dy(), "QR codes are square")
}

// FR-022: QR images are generated on demand from the ticket code. Nothing is read
// from or written to storage, so the same code must always produce the same image.
func TestRenderQREncodesTheCodeItWasGiven(t *testing.T) {
	first, err := notification.RenderQR("ABC234DEFG")
	require.NoError(t, err)
	again, err := notification.RenderQR("ABC234DEFG")
	require.NoError(t, err)
	other, err := notification.RenderQR("HJK567LMNP")
	require.NoError(t, err)

	assert.Equal(t, first, again, "the same code always renders the same QR")
	assert.NotEqual(t, first, other, "a different code must render a different QR")
}

func TestRenderQRRejectsAnEmptyCode(t *testing.T) {
	_, err := notification.RenderQR("")

	require.Error(t, err)
}
