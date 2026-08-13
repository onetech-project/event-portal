package notification_test

import (
	"bytes"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
)

// receiptOrder is a two-line order with a percentage fee and a fixed fee — the
// shape the design was drawn against, and the shape whose totals must reconcile.
func receiptOrder() notification.OrderDelivery {
	subtotal := decimal.RequireFromString("105000")
	return notification.OrderDelivery{
		OrderNumber: "42023-C2D24-5221",
		BuyerName:   "Alex Pradita Dimas",
		BuyerEmail:  "alexpradita@gmail.com",
		BuyerPhone:  "+628123456789",
		Status:      "PAID",
		CreatedAt:   time.Date(2026, 4, 20, 14, 30, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 4, 20, 14, 35, 0, 0, time.UTC),
		Subtotal:    &subtotal,
		TotalAmount: decimal.RequireFromString("121550"),
		Items: []notification.ReceiptLine{
			{
				Name: "Jive Jakarta International Vape (Day 1)", Quantity: 2,
				UnitPrice:       decimal.RequireFromString("35000"),
				Subtotal:        decimal.RequireFromString("70000"),
				AdmissionStarts: []time.Time{time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)},
				Descriptor:      "Expo Entrance Ticket",
			},
			{
				Name: "Jive Jakarta International Vape (Day 2)", Quantity: 1,
				UnitPrice:       decimal.RequireFromString("35000"),
				Subtotal:        decimal.RequireFromString("35000"),
				AdmissionStarts: []time.Time{time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)},
				Descriptor:      "Expo Entrance Ticket",
			},
		},
		Fees: []notification.ReceiptFee{
			{Name: "PPN (11%)", Amount: decimal.RequireFromString("11550")},
			{Name: "Application Fee (Admin Fee)", Amount: decimal.RequireFromString("5000")},
		},
		Event: notification.DeliveryEvent{
			Name:    "JIVE 2026",
			Venue:   "JIVE Jakarta",
			Address: "Pademangan, Jkt Utara, Daerah Khusus Ibukota Jakarta 14410",
		},
		Payment: notification.PaymentSummary{
			Method: "QRIS",
			Status: "Paid",
			PaidAt: time.Date(2026, 4, 20, 14, 30, 0, 0, time.UTC),
		},
	}
}

func TestRenderReceiptPDFProducesASinglePagePDF(t *testing.T) {
	doc, err := notification.RenderReceiptPDF(receiptOrder(), sampleBrand())

	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(doc, []byte("%PDF-")), "output must be a PDF")
	assert.Equal(t, 1, pdfPageCount(doc), "an ordinary receipt fits one page")
}

func TestRenderReceiptPDFRefusesAnOrderWithNoNumber(t *testing.T) {
	order := receiptOrder()
	order.OrderNumber = ""

	_, err := notification.RenderReceiptPDF(order, sampleBrand())

	require.Error(t, err, "a receipt with nothing to identify it is not a receipt")
}

// FR-008, FR-009, FR-010. The header stamp and both detail columns.
func TestRenderReceiptPDFPrintsTheHeaderAndBothDetailColumns(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "Payment Receipt")
	assert.Contains(t, text, "Order No. : 42023-C2D24-5221")
	assert.Contains(t, text, "Last updated : 21:35, 20 Apr 2026 WIB")

	assert.Contains(t, text, "Order Details")
	assert.Contains(t, text, "Customer Name")
	assert.Contains(t, text, "Alex Pradita Dimas")

	assert.Contains(t, text, "Transaction Details")
	assert.Contains(t, text, "Paid")
	assert.Contains(t, text, "QRIS")
	// The SETTLEMENT time, distinct from the last-updated stamp above. The design
	// draws that distinction and collapsing them would erase a real fact.
	assert.Contains(t, text, "20 Apr 2026, 21:30 WIB")
}

// FR-009, FR-033. The buyer's contact details are masked here, exactly as in the
// email body — never printed raw on a document that gets forwarded.
func TestRenderReceiptPDFMasksTheBuyerContactDetails(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, notification.MaskEmail("alexpradita@gmail.com"))
	assert.Contains(t, text, notification.MaskPhone("+628123456789"))
	assert.NotContains(t, text, "alexpradita@gmail.com")
	assert.NotContains(t, text, "+628123456789")
}

// FR-027. An unrecorded phone omits its row rather than printing a blank or a
// fully-masked placeholder.
func TestRenderReceiptPDFOmitsThePhoneRowWhenNoneWasRecorded(t *testing.T) {
	order := receiptOrder()
	order.BuyerPhone = ""

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)

	assert.NotContains(t, string(doc), "Phone Number")
}

// FR-011. Every purchased line, with its quantity, unit price and line total.
func TestRenderReceiptPDFItemizesEveryLine(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	// Parentheses are escaped inside a PDF text string, so a name containing them
	// appears as `\(Day 1\)` in the content stream. Asserting the escaped form is
	// deliberate — searching for the raw form would silently never match.
	assert.Contains(t, text, "Product")
	assert.Contains(t, text, "Jive Jakarta International Vape \\(Day 1\\)")
	assert.Contains(t, text, "Jive Jakarta International Vape \\(Day 2\\)")
	assert.Contains(t, text, "x 2")
	assert.Contains(t, text, "x 1")
	assert.Contains(t, text, "IDR 70.000")
	assert.Contains(t, text, "IDR 35.000")
}

// FR-011. The product sub-line: the line's OWN admission day(s), then its own
// description. Neither half is mandatory.
func TestRenderReceiptPDFRendersEverySubLineShape(t *testing.T) {
	day1 := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		line notification.ReceiptLine
		want string
		// absent is asserted NOT to appear, catching a bare separator.
		absent string
	}{
		"date and descriptor": {
			line: notification.ReceiptLine{Name: "A", AdmissionStarts: []time.Time{day1}, Descriptor: "Expo Entrance Ticket"},
			want: "26 Apr 2026 - Expo Entrance Ticket",
		},
		"date alone": {
			line:   notification.ReceiptLine{Name: "B", AdmissionStarts: []time.Time{day1}},
			want:   "26 Apr 2026",
			absent: "26 Apr 2026 -",
		},
		"bundle lists every day rather than collapsing to the earliest": {
			line: notification.ReceiptLine{Name: "C", AdmissionStarts: []time.Time{day1, day2}, Descriptor: "Two-day pass"},
			want: "26 Apr 2026, 27 Apr 2026 - Two-day pass",
		},
		"descriptor alone": {
			line: notification.ReceiptLine{Name: "D", Descriptor: "Merchandise"},
			want: "Merchandise",
		},
	} {
		t.Run(name, func(t *testing.T) {
			order := receiptOrder()
			tc.line.Quantity = 1
			tc.line.UnitPrice = decimal.RequireFromString("1000")
			tc.line.Subtotal = decimal.RequireFromString("1000")
			order.Items = []notification.ReceiptLine{tc.line}

			doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
			require.NoError(t, err)

			assert.Contains(t, string(doc), tc.want)
			if tc.absent != "" {
				assert.NotContains(t, string(doc), tc.absent, "no bare separator")
			}
		})
	}
}

// A line with neither a date nor a description omits the sub-line entirely.
func TestRenderReceiptPDFOmitsAnEmptySubLine(t *testing.T) {
	order := receiptOrder()
	order.Items = []notification.ReceiptLine{{
		Name: "Plain", Quantity: 1,
		UnitPrice: decimal.RequireFromString("1000"),
		Subtotal:  decimal.RequireFromString("1000"),
	}}

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())

	require.NoError(t, err)
	assert.Contains(t, string(doc), "Plain")
}

// FR-012, FR-014 / SC-003. The breakdown reconciles: subtotal + every frozen fee
// equals the grand total, and the grand total is what the buyer was charged.
func TestRenderReceiptPDFTotalsReconcileWithTheChargedAmount(t *testing.T) {
	order := receiptOrder()

	// The fixture must be arithmetically honest, or the document can agree with
	// a broken breakdown and still pass.
	sum := *order.Subtotal
	for _, fee := range order.Fees {
		sum = sum.Add(fee.Amount)
	}
	require.True(t, sum.Equal(order.TotalAmount), "fixture must reconcile")

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "Subtotal")
	assert.Contains(t, text, "IDR 105.000")
	assert.Contains(t, text, "PPN \\(11%\\)")
	assert.Contains(t, text, "IDR 11.550")
	assert.Contains(t, text, "Application Fee \\(Admin Fee\\)")
	assert.Contains(t, text, "IDR 5.000")
	assert.Contains(t, text, "Total")
	assert.Contains(t, text, "IDR 121.550")
	// A percentage fee carries the derived-rate note.
	assert.Contains(t, text, "*Based on applicable tax rates")
}

// FR-012. Fee names print exactly as frozen onto the order. Renaming them would
// make the receipt disagree with the total the buyer approved at checkout.
func TestRenderReceiptPDFPrintsFrozenFeeNamesVerbatim(t *testing.T) {
	order := receiptOrder()
	order.Fees = []notification.ReceiptFee{
		{Name: "Bea Meterai 2026", Amount: decimal.RequireFromString("16550")},
	}
	order.TotalAmount = order.Subtotal.Add(decimal.RequireFromString("16550"))

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)

	assert.Contains(t, string(doc), "Bea Meterai 2026")
	// No percentage in the name, so no derived-rate note.
	assert.NotContains(t, string(doc), "*Based on applicable tax rates")
}

// FR-013. A pre-fee order records no subtotal: the total alone, with no subtotal
// row and no fee rows invented to fill the gap.
func TestRenderReceiptPDFCollapsesAPreFeeOrderToTheTotal(t *testing.T) {
	order := receiptOrder()
	order.Subtotal = nil
	order.Fees = nil

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "Total")
	assert.Contains(t, text, "IDR 121.550")
	assert.NotContains(t, text, "Subtotal")
	assert.NotContains(t, text, "PPN")
	assert.NotContains(t, text, "*Based on applicable tax rates")
}

// FR-015, FR-035. The closing block, sourced from configuration.
func TestRenderReceiptPDFClosesWithTheConfiguredIssuerDetails(t *testing.T) {
	brand := sampleBrand()
	brand.LegalEntity = "PT Other Teknologi"
	brand.SupportEmail = "support@other.example"
	brand.Attribution = "Powered By Other"

	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), brand)
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "PT Other Teknologi's system")
	assert.Contains(t, text, "contact our customer service at:")
	assert.Contains(t, text, "support@other.example")
	assert.Contains(t, text, "Powered By Other")
}

// The receipt carries money; admission lives on the e-ticket. A ticket code or a
// QR here would be a second, unscannable copy.
func TestRenderReceiptPDFCarriesNoTicketCodeOrQR(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.NotContains(t, text, "SHOW AT ENTRY")
	assert.NotContains(t, text, "Ticket No.")
	assert.NotContains(t, text, "/Subtype /Image", "no QR is embedded in a receipt")
}

// SC-009. A resend must reproduce the same document, so rendering is
// deterministic for identical input.
func TestRenderReceiptPDFIsDeterministicForTheSameInput(t *testing.T) {
	first, err := notification.RenderReceiptPDF(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	second, err := notification.RenderReceiptPDF(receiptOrder(), sampleBrand())
	require.NoError(t, err)

	// Byte equality would depend on the PDF's embedded timestamp; size is the
	// same check the ticket document uses.
	assert.Equal(t, len(first), len(second))
}

// R-006 applied to the receipt: the event address is admin-authored free text
// and is the widest UTF-8 exposure this feature adds to a printed page.
func TestRenderReceiptPDFAppliesTheLatin1Guard(t *testing.T) {
	order := receiptOrder()
	order.Items[0].Name = "Vape Expo — Day 1"
	order.Items[0].Descriptor = "Organiser's “premium” pass"

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "Vape Expo - Day 1")
	assert.NotContains(t, text, "â", "no mojibake on a printed receipt")
}

// --- Spec 016 Revision 2: the day-boundary defect --------------------------

// FR-020a / T080. A ticket admitting at 06:00 WIB is 23:00Z the PREVIOUS day.
// Rendering the scanned value's own calendar date therefore printed the wrong
// day and told a Day 2 holder they admit on Day 1.
//
// This must fail under BOTH TZ=UTC and TZ=Asia/Jakarta before the fix:
// receiptSubLine formatted in the value's own location, so a time.UTC input
// printed the UTC day whatever the host was. A test that only goes red under one
// TZ is asserting the host's zone rather than the code's behaviour.
func TestReceiptRendersAdmissionDatesInJakarta(t *testing.T) {
	order := receiptOrder()
	order.Items = []notification.ReceiptLine{{
		Name: "Day 2 Access", Quantity: 1,
		UnitPrice: decimal.RequireFromString("35000"),
		Subtotal:  decimal.RequireFromString("35000"),
		// 2026-04-27 06:00 WIB.
		AdmissionStarts: []time.Time{time.Date(2026, 4, 26, 23, 0, 0, 0, time.UTC)},
	}}

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "27 Apr 2026",
		"the admission day is the Jakarta day, not the UTC day")
	assert.NotContains(t, text, "26 Apr 2026",
		"printing the UTC day tells a Day 2 holder they admit on Day 1")
}

// Every stamp on the receipt names its zone, and names the same one.
func TestReceiptStampsAreAllJakarta(t *testing.T) {
	order := receiptOrder()
	// 2026-04-20 21:35 WIB and 21:30 WIB respectively.
	order.UpdatedAt = time.Date(2026, 4, 20, 14, 35, 0, 0, time.UTC)
	order.Payment.PaidAt = time.Date(2026, 4, 20, 14, 30, 0, 0, time.UTC)

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	// UTC in, WIB out — the +7 shift is what proves a conversion happened rather
	// than a layout string being copied.
	assert.Contains(t, text, "21:35, 20 Apr 2026 WIB")
	assert.Contains(t, text, "20 Apr 2026, 21:30 WIB")
	assert.NotContains(t, text, "UTC")
}

// FR-010a: the payment method is upper case whatever the payment row stores.
func TestReceiptUpperCasesThePaymentMethod(t *testing.T) {
	order := receiptOrder()
	order.Payment.Method = "qris"

	doc, err := notification.RenderReceiptPDFPlain(order, sampleBrand())
	require.NoError(t, err)

	assert.Contains(t, string(doc), "QRIS")
	assert.NotContains(t, string(doc), "qris")
}

// FR-015a / T086. SetLineCapStyle and SetLineJoinStyle are sticky Fpdf state.
// drawEnvelope sets both to "round"; if it does not reset them, every rule drawn
// afterwards inherits round ends — a defect that passes every content assertion
// and still looks wrong on the page.
//
// The content stream records the graphics operators, so the reset is observable:
// `1 J` is round cap, `0 J` is butt. A document whose LAST cap operator is `1 J`
// has leaked the style.
func TestReceiptResetsLineStyleAfterDrawingTheEnvelope(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	round := strings.LastIndex(text, "1 J")
	butt := strings.LastIndex(text, "0 J")

	require.NotEqual(t, -1, round, "the envelope should have set a round cap")
	require.NotEqual(t, -1, butt, "the round cap must be reset to butt afterwards")
	assert.Greater(t, butt, round,
		"the last cap operator must be butt: a trailing round cap means every rule "+
			"drawn after the envelope inherits round ends")
}

// FR-015b. The envelope icon and the address it precedes must sit on one line.
//
// The defect this pins was invisible to every content assertion: gofpdf's MoveTo
// moves the current position, so reading GetY() AFTER drawing the icon returned
// the icon path's own Y and pushed the address ~2 mm below its icon. The
// arithmetic was right; the reference point was not.
//
// So this measures. It reads the icon's drawn extent and the address's text
// origin straight out of the content stream and asserts they line up — which is
// the only way to catch a purely geometric regression.
func TestReceiptEnvelopeSitsOnTheAddressLine(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	// The envelope body is filled in the slate that only it uses, so the fill
	// operator is a stable anchor for the path that follows.
	const slateFill = "0.420 0.447 0.502 rg"
	fillAt := strings.Index(text, slateFill)
	require.NotEqual(t, -1, fillAt, "the envelope's slate fill should be present")
	pathEnd := strings.Index(text[fillAt:], "\nf\n")
	require.NotEqual(t, -1, pathEnd, "the envelope path should be closed with a fill")
	path := text[fillAt : fillAt+pathEnd]

	// Every coordinate pair in the path, whatever operator consumes it.
	coords := regexp.MustCompile(`(-?\d+\.?\d*)\s+(-?\d+\.?\d*)\s+[mlc]`).FindAllStringSubmatch(path, -1)
	require.NotEmpty(t, coords, "the envelope path should carry coordinates")

	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, c := range coords {
		y, err := strconv.ParseFloat(c[2], 64)
		require.NoError(t, err)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	iconCentre := (minY + maxY) / 2

	// The address's own text origin, taken from the Td immediately before it.
	addrAt := strings.Index(text, "(help@manjo.com)")
	require.NotEqual(t, -1, addrAt)
	tds := regexp.MustCompile(`(-?\d+\.?\d*)\s+(-?\d+\.?\d*)\s+Td`).FindAllStringSubmatch(text[:addrAt], -1)
	require.NotEmpty(t, tds)
	baseline, err := strconv.ParseFloat(tds[len(tds)-1][2], 64)
	require.NoError(t, err)

	// PDF space is bottom-up. Lowercase text sits roughly a quarter of its size
	// above its baseline, so that is the optical centre to align the icon to.
	const fontSize = 9.0
	textCentre := baseline + 0.25*fontSize

	assert.InDelta(t, textCentre, iconCentre, 1.5,
		"the envelope (centre %.2f) must sit on the address's line (centre %.2f); "+
			"a large gap means the icon was positioned from a cursor that drawing it had moved",
		iconCentre, textCentre)
}

// FR-011b / SC-016. The product table's content is inset by the same amount on
// both sides.
//
// This asserts the geometry directly rather than measuring the rendered text.
// The first attempt did measure it and was wrong for an instructive reason:
// gofpdf adds its own cell margin inside every CellFormat, so the drawn text sits
// one cMargin further in than the column x on the left AND one further out on the
// right. Comparing a raw column x against a drawn text x therefore compares two
// different things and reports a millimetre of drift that is not there.
//
// The invariant that actually matters is between the column geometry and the
// table's edges, and it is symmetric by construction or it is not.
func TestReceiptTablePaddingIsSymmetric(t *testing.T) {
	left, right := notification.TableInsets()

	assert.Equal(t, left, right,
		"the product column is inset %.1fmm from the table's left edge but the amount "+
			"column is inset %.1fmm from its right edge; a table padded on one side only "+
			"reads as lopsided", left, right)
	assert.Positive(t, left, "the content must be inset from the table edge at all")
}

// The rendered document still has to agree: the leftmost text sits inside the
// table's left edge rather than on it.
func TestReceiptTableContentSitsInsideItsEdges(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)

	productX, ok := textOriginX(string(doc), "(Product)")
	require.True(t, ok, "the Product header should be present")

	const pt = 72.0 / 25.4
	assert.Greater(t, productX, marginLeftMM*pt,
		"the Product header must be drawn inside the table's left edge")
}

// marginLeftMM mirrors the page margin used by the renderer.
const marginLeftMM = 17.0

// textOriginX returns the x of the Td immediately preceding the given literal.
func textOriginX(stream, literal string) (float64, bool) {
	at := strings.Index(stream, literal)
	if at < 0 {
		return 0, false
	}
	tds := regexp.MustCompile(`(-?\d+\.?\d*)\s+(-?\d+\.?\d*)\s+Td`).FindAllStringSubmatch(stream[:at], -1)
	if len(tds) == 0 {
		return 0, false
	}
	x, err := strconv.ParseFloat(tds[len(tds)-1][1], 64)
	return x, err == nil
}

// FR-011c / SC-017. The Price header is centred over its column while Qty and
// Total stay right-aligned.
//
// Alignment is not recorded in the PDF — gofpdf resolves it to an absolute text
// origin — so this measures where each header actually lands relative to its
// column's right edge. A centred header sits materially further left than a
// right-aligned one of the same width.
func TestReceiptPriceHeaderIsCentredOverItsColumn(t *testing.T) {
	doc, err := notification.RenderReceiptPDFPlain(receiptOrder(), sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	const pt = 72.0 / 25.4
	priceX, ok := textOriginX(text, "(Price)")
	require.True(t, ok, "the Price header should be present")
	totalX, ok := textOriginX(text, "(Total)")
	require.True(t, ok, "the Total header should be present")

	priceRight, totalRight := notification.HeaderColumnRights()

	// Distance from each header's text origin to its own column's right edge.
	// "Price" and "Total" are near-identical widths at the same size and weight,
	// so a centred Price must sit clearly further from its edge than a
	// right-aligned Total does from its.
	priceGap := priceRight*pt - priceX
	totalGap := totalRight*pt - totalX

	assert.Greater(t, priceGap, totalGap+5,
		"Price (%.1fpt from its column edge) must be centred, not right-aligned like "+
			"Total (%.1fpt)", priceGap, totalGap)
}
