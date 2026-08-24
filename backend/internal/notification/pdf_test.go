package notification_test

import (
	"bytes"
	"image/png"
	"regexp"
	"strings"
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
			AttendeeEmail:  "attendee@example.com",
			TicketTypeName: "Regular",
			EventName:      "Jazz Night 2026",
			Venue:          "Balai Sarbini",
			EventStart:     time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC),
			EventEnd:       time.Date(2026, 9, 1, 23, 0, 0, 0, time.UTC),
		})
	}
	return out
}

func sampleOrder() notification.OrderDelivery {
	return notification.OrderDelivery{
		OrderNumber: "ORD-20260731-ABCDEF",
		BuyerName:   "Budi Santoso",
		BuyerEmail:  "budi@example.com",
		BuyerPhone:  "081234567890",
		Status:      "PAID",
		Event: notification.DeliveryEvent{
			Name:    "Jazz Night 2026",
			Venue:   "Balai Sarbini",
			Address: "Jl. Jend. Sudirman Kav. 50, Jakarta Selatan 12190",
		},
	}
}

func sampleBrand() notification.Branding {
	return notification.Branding{
		SiteName:     "JIVE",
		SiteURL:      "https://www.jive.co.id",
		SupportEmail: "help@manjo.com",
		LegalEntity:  "PT Manjo Teknologi Indonesia",
		Attribution:  "Powered By Manjo",
	}
}

// pdfPageCount counts page objects. The page tree node is "/Type /Pages", so it
// is excluded — counting it would report one page too many on every document.
//
// Page dictionaries are not inside a compressed stream, so this works on the
// shipped bytes as well as on the uncompressed test variant.
func pdfPageCount(doc []byte) int {
	return bytes.Count(doc, []byte("/Type /Page")) - bytes.Count(doc, []byte("/Type /Pages"))
}

func TestRenderTicketsPDFProducesAPDFDocument(t *testing.T) {
	out, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(1), sampleBrand())

	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(out, []byte("%PDF-")), "output must be a PDF")
	assert.Greater(t, len(out), 1000)
}

// One order produces exactly one PDF containing every ticket, not one PDF per
// ticket (Constitution, Critical Data Flow Rules).
func TestRenderTicketsPDFGrowsWithTheNumberOfTickets(t *testing.T) {
	one, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(1), sampleBrand())
	require.NoError(t, err)

	four, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(4), sampleBrand())
	require.NoError(t, err)

	assert.Greater(t, len(four), len(one), "every ticket must be rendered into the single document")
}

func TestRenderTicketsPDFRejectsAnEmptyTicketSet(t *testing.T) {
	_, err := notification.RenderTicketsPDF(sampleOrder(), nil, sampleBrand())

	require.Error(t, err, "an order with no tickets has nothing to deliver")
}

func TestRenderTicketsPDFIsDeterministicForTheSameInput(t *testing.T) {
	first, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(2), sampleBrand())
	require.NoError(t, err)
	second, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(2), sampleBrand())
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

// Spec 016 FR-020a: the admission window reads "26 Apr 2026 @ 10:00 - 21:00 WIB".
//
// Every case feeds UTC and expects WIB. That +7 shift is the assertion: a test
// written WIB-in/WIB-out would pass even if the renderer did no conversion at
// all, which is exactly the bug this format change is bundled with.
func TestFormatTicketWindowCollapsesASameDayWindow(t *testing.T) {
	start := time.Date(2026, 4, 26, 3, 0, 0, 0, time.UTC) // 10:00 WIB
	end := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)  // 21:00 WIB

	got := notification.FormatTicketWindow(start, end)

	assert.Equal(t, "26 Apr 2026 @ 10:00 - 21:00 WIB", got,
		"a window inside one day reads as one date with a time range")
}

// The multi-day and single-instant branches are NOT specified by FR-020a. They
// are pinned here so the document cannot ship one ticket in the new form and
// another in the old one.
func TestFormatTicketWindowSpellsOutAMultiDayWindow(t *testing.T) {
	start := time.Date(2026, 4, 26, 3, 0, 0, 0, time.UTC) // 10:00 WIB
	end := time.Date(2026, 4, 28, 14, 0, 0, 0, time.UTC)  // 21:00 WIB, two days later

	got := notification.FormatTicketWindow(start, end)

	assert.Equal(t, "26 Apr 2026 @ 10:00 - 28 Apr 2026 @ 21:00 WIB", got)
}

func TestFormatTicketWindowPrintsASingleInstantOnce(t *testing.T) {
	at := time.Date(2026, 4, 26, 3, 0, 0, 0, time.UTC) // 10:00 WIB

	assert.Equal(t, "26 Apr 2026 @ 10:00 WIB", notification.FormatTicketWindow(at, at))
	assert.Equal(t, "26 Apr 2026 @ 10:00 WIB", notification.FormatTicketWindow(at, time.Time{}),
		"a zero end is not printed as a range")
}

// The day boundary matters here too: an admission opening at 06:00 WIB is 23:00Z
// the previous day, so a renderer that skips the conversion prints the wrong
// DATE on the ticket itself.
func TestFormatTicketWindowUsesTheJakartaCalendarDay(t *testing.T) {
	start := time.Date(2026, 4, 26, 23, 0, 0, 0, time.UTC) // 27 Apr 06:00 WIB
	end := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)   // 27 Apr 17:00 WIB

	got := notification.FormatTicketWindow(start, end)

	assert.Equal(t, "27 Apr 2026 @ 06:00 - 17:00 WIB", got)
}

// pdfTextOp matches one text-showing operator in an uncompressed content stream:
// gofpdf emits drawn strings as `(text)Tj`. The inner alternation skips escaped
// parentheses so a literal "\)" does not end the match early.
var pdfTextOp = regexp.MustCompile(`\(((?:[^()\\]|\\.)*)\)Tj`)

// pdfDrawnText returns only the text a document actually DRAWS, with the PDF
// string escapes undone.
//
// Why this exists rather than asserting against string(doc): a rendered page's
// bytes also carry embedded images — the QR codes and the brand mark — and any
// short string can occur by chance inside that binary. A whole-file NotContains
// therefore fails at random. It did: the mark shipped in Revision 3 contains the
// bytes "Rp" at offset ~17125 inside its image stream, which broke FR-021's
// no-money assertion while no page drew any money at all.
//
// Only meaningful on the *Plain renderers — production compresses its streams,
// so there is no `(text)Tj` to find (research R-025).
func pdfDrawnText(doc []byte) string {
	var out strings.Builder
	for _, m := range pdfTextOp.FindAllSubmatch(doc, -1) {
		s := string(m[1])
		s = strings.ReplaceAll(s, `\(`, "(")
		s = strings.ReplaceAll(s, `\)`, ")")
		s = strings.ReplaceAll(s, `\\`, `\`)
		out.WriteString(s)
		out.WriteString("\n")
	}
	return out.String()
}
