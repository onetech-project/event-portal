// Package notification renders ticket PDFs and delivers them by email. It owns no
// database tables — it reaches the order and ticket domains through the narrow
// interfaces declared in service.go (Constitution Principle II).
package notification

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	qrcode "github.com/skip2/go-qrcode"
)

// qrPixelSize is the rendered PNG's edge length. It is generous enough that the
// image stays sharp when the PDF is printed rather than scanned from a screen.
const qrPixelSize = 512

// TicketDetail is everything printed on one ticket.
//
// AttendeeEmail IS printed, on the page's identity column (spec 016 FR-018). It
// is shown in full, unlike the buyer contact details on the receipt and the email
// body: this is holder identity on a document presented at a gate, not a contact
// detail on a receipt that gets forwarded (FR-034).
type TicketDetail struct {
	TicketCode     string
	AttendeeName   string
	AttendeeEmail  string
	TicketTypeName string
	EventName      string
	Venue          string
	// The ticket type's own admission window (spec 015). A Day 2 pass prints
	// Day 2, not the festival's opening date.
	EventStart time.Time
	EventEnd   time.Time
}

// FormatTicketWindow renders a ticket's admission window: "26 Apr 2026 @ 10:00 -
// 21:00 WIB" (spec 016 FR-020a).
//
// A window that opens and closes on the same day — the ordinary case — reads as
// one date with a time range rather than as two near-identical datetimes, which
// is what a holder glancing at a ticket at the gate actually needs.
//
// Both endpoints are converted to Jakarta FIRST, and the same-day comparison is
// made on the converted values. Comparing the raw ones would split a window that
// crosses midnight UTC but not midnight WIB, and would print the wrong calendar
// date besides: an admission opening at 06:00 WIB is 23:00Z the previous day.
//
// The zone is printed once, at the end. FR-020a specifies only the same-day
// form; the multi-day and single-instant forms are chosen here to match it
// rather than left on the old layout, so one order cannot produce one ticket
// reading "26 Apr 2026 @ 10:00 - 21:00 WIB" and another reading
// "Wed, 02 Sep 2026 09:00 UTC".
func FormatTicketWindow(start, end time.Time) string {
	const dayTime = "02 Jan 2006 @ 15:04"

	from := inJakarta(start)
	if end.IsZero() || end.Equal(start) {
		return from.Format(dayTime + " MST")
	}

	to := inJakarta(end)
	sy, sm, sd := from.Date()
	ey, em, ed := to.Date()
	if sy == ey && sm == em && sd == ed {
		return fmt.Sprintf("%s - %s", from.Format(dayTime), to.Format("15:04 MST"))
	}
	return fmt.Sprintf("%s - %s", from.Format(dayTime), to.Format(dayTime+" MST"))
}

// RenderQR draws a ticket code as a QR PNG in memory.
//
// The image is generated on demand every time it is needed — initial delivery,
// resend, or any future download. This MVP has no object storage, so
// tickets.qr_code_url stays NULL and nothing is ever written to disk (spec FR-022,
// constitution Technology Stack Requirements).
func RenderQR(code string) ([]byte, error) {
	if code == "" {
		return nil, errors.New("notification: cannot render a QR for an empty ticket code")
	}

	png, err := qrcode.Encode(code, qrcode.Medium, qrPixelSize)
	if err != nil {
		return nil, fmt.Errorf("render QR for ticket code: %w", err)
	}
	return png, nil
}

// RenderTicketsPDF builds the single PDF an order's buyer receives, with one page
// per ticket. Exactly one document is produced per order regardless of ticket
// count (constitution, Critical Data Flow Rules).
func RenderTicketsPDF(order OrderDelivery, tickets []TicketDetail, brand Branding) ([]byte, error) {
	return renderTicketsPDF(order, tickets, brand, true)
}

// renderTicketsPDF carries the compress flag the test seam in export_test.go
// needs: gofpdf compresses content streams, so what a page SAYS is unreadable in
// the shipped bytes. Production always compresses.
func renderTicketsPDF(order OrderDelivery, tickets []TicketDetail, brand Branding, compress bool) ([]byte, error) {
	if len(tickets) == 0 {
		return nil, errors.New("notification: cannot render a ticket PDF with no tickets")
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(compress)
	// One page per ticket is a hard requirement (FR-002 / SC-002), and this
	// layout is absolutely positioned, footer band included. Left on, gofpdf's
	// automatic break fires the moment anything is drawn below the default
	// bottom margin — the footer band always is — and silently turns each ticket
	// into three pages.
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle("E-Ticket "+order.OrderNumber, true)
	pdf.SetAuthor(brandOr(brand.SiteName, "Event Ticketing"), true)

	for i, ticket := range tickets {
		if err := renderTicketPage(pdf, order, ticket, i, len(tickets), brand); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("write ticket PDF: %w", err)
	}
	return buf.Bytes(), nil
}

// The palette, mirrored from the frontend brand tokens so the printed documents
// match the site and the email.
var (
	pdfBrand = [3]int{203, 28, 79}
	pdfInk   = [3]int{24, 24, 27}
	pdfLine  = [3]int{224, 224, 228}
	pdfGray  = [3]int{110, 110, 110}
	pdfBand  = [3]int{21, 26, 38} // #151A26 — the design's band AND the logo's own
	// opaque background. The staged logo has zero transparent pixels, so any other
	// band colour draws a visible rectangle around it.
	pdfBandFg  = [3]int{236, 238, 243}
	pdfBandDim = [3]int{150, 156, 172}
	// The accent rule beside the QR panel: #334155, the design's slate. Not
	// pdfBrand — this is structure, not brand accent.
	pdfAccent = [3]int{51, 65, 85}
)

// A4 portrait geometry, in mm. Named because three renderers share them and a
// stray literal is how two documents drift apart.
const (
	pageWidth   = 210.0
	pageHeight  = 297.0
	marginLeft  = 17.0
	marginRight = 17.0
	contentWide = pageWidth - marginLeft - marginRight
	bandHeight  = 21.0
)

// renderTicketPage draws one e-ticket in the Figma 683-148 layout: a dark header
// band with the brand mark, the "Ticket N of M" heading, the QR beside the
// ticket/holder/order identity column, the event and ticket-type names, the
// admission window, and a dark footer band with the site and support contacts.
//
// It prints NO price and no fee (spec 016 FR-021): the revised design removed
// them, and money belongs to the receipt. That also dissolves the package-line
// problem the old layout had — a ticket issued from a bundle has no per-ticket
// price that could honestly be printed.
func renderTicketPage(pdf *gofpdf.Fpdf, order OrderDelivery, ticket TicketDetail, index, total int, brand Branding) error {
	pdf.AddPage()

	drawBand(pdf, 0, bandHeight)
	drawBrandMark(pdf, brand, pageWidth-marginRight, 0, bandHeight)

	// "Ticket N of M" — the pagination the merged document needs so a holder can
	// tell at a glance whether they have all of them (FR-016).
	setColor(pdf, pdfInk)
	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetXY(marginLeft, 29)
	pdf.CellFormat(contentWide, 9, latin1(fmt.Sprintf("Ticket %d of %d", index+1, total)), "", 1, "L", false, 0, "")

	// The accent bar down the left edge, alongside the identity block. It sits
	// FLUSH against the page edge, so only its right corners are rounded — "23"
	// is top-right and bottom-right. Rounding the left pair would round corners
	// nobody can see and leave the bar looking detached from the edge.
	pdf.SetFillColor(pdfAccent[0], pdfAccent[1], pdfAccent[2])
	pdf.RoundedRect(0, 40, 3.0, 72, 1.5, "23", "F")

	// QR panel, left.
	const qrX, qrY, qrSize = 22.0, 46.0, 62.0
	png, err := RenderQR(ticket.TicketCode)
	if err != nil {
		return err
	}
	// A distinct image name per page: gofpdf caches registered images by name, so
	// reusing one name would print the first ticket's QR on every page.
	imageName := fmt.Sprintf("qr-%d-%s", index, ticket.TicketCode)
	pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	pdf.ImageOptions(imageName, qrX, qrY, qrSize, qrSize, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")

	// Identity column, right of the QR (FR-017, FR-018).
	const idX = 100.0
	idWidth := pageWidth - marginRight - idX
	y := qrY
	labelledValue := func(label, value string) {
		setColor(pdf, pdfGray)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.SetXY(idX, y)
		pdf.CellFormat(idWidth, 4, latin1(strings.ToUpper(label)), "", 1, "L", false, 0, "")
		setColor(pdf, pdfInk)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetXY(idX, y+4.5)
		pdf.MultiCell(idWidth, 5.5, latin1(value), "", "L", false)
		y = pdf.GetY() + 4.5
	}
	labelledValue("Ticket No.", ticket.TicketCode)
	labelledValue("Name", ticket.AttendeeName)
	labelledValue("Email", ticket.AttendeeEmail)
	labelledValue("Order No.", order.OrderNumber)

	// Event name above the ticket-type headline (FR-019).
	setColor(pdf, pdfBrand)
	pdf.SetFont("Helvetica", "B", 9.5)
	pdf.SetXY(marginLeft, 132)
	pdf.MultiCell(contentWide, 5, latin1(strings.ToUpper(ticket.EventName)), "", "L", false)

	setColor(pdf, pdfInk)
	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetX(marginLeft)
	pdf.MultiCell(contentWide, 9, latin1(ticket.TicketTypeName), "", "L", false)

	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.SetLineWidth(0.3)
	dividerY := pdf.GetY() + 5
	pdf.Line(marginLeft, dividerY, pageWidth-marginRight, dividerY)

	// VALID FOR — this TICKET TYPE's admission window, never the parent event's
	// (FR-020, constitution Critical Data Flow Rules).
	setColor(pdf, pdfGray)
	pdf.SetFont("Helvetica", "", 7.5)
	pdf.SetXY(marginLeft, dividerY+8)
	pdf.CellFormat(60, 5, "VALID FOR", "", 0, "L", false, 0, "")
	setColor(pdf, pdfInk)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetXY(marginLeft+76, dividerY+7)
	pdf.MultiCell(contentWide-76, 6, latin1(FormatTicketWindow(ticket.EventStart, ticket.EventEnd)), "", "L", false)

	// No venue row (FR-019a). The design has no such field. Recorded as a
	// deliberate trade rather than an oversight: a holder arriving at a gate no
	// longer has the address on the pass itself, and it remains on the email body
	// and the confirmation screen.

	drawTicketFooter(pdf, brand)
	return pdf.Error()
}

// drawTicketFooter draws the dark closing band: the site on the left, the
// customer-service address on the right (FR-022).
func drawTicketFooter(pdf *gofpdf.Fpdf, brand Branding) {
	const footerTop = pageHeight - 25
	drawBand(pdf, footerTop, 25)

	setColor(pdf, pdfBandFg)
	pdf.SetFont("Helvetica", "B", 7.5)
	pdf.SetXY(marginLeft, footerTop+7)
	pdf.CellFormat(80, 4, latin1(strings.ToUpper(brand.SiteName)), "", 1, "L", false, 0, "")
	setColor(pdf, pdfBandDim)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetX(marginLeft)
	pdf.CellFormat(80, 5, latin1(brand.SiteURL), "", 1, "L", false, 0, "")

	// Right-ALIGNED to the right margin, not merely starting from a fixed x: the
	// two blocks sit at opposite edges of the band, so the footer reads as
	// justified however long either string is.
	const rightX = 110.0
	rightWidth := pageWidth - marginRight - rightX
	setColor(pdf, pdfBandFg)
	pdf.SetFont("Helvetica", "B", 7.5)
	pdf.SetXY(rightX, footerTop+7)
	pdf.CellFormat(rightWidth, 4, "CUSTOMER SERVICE", "", 1, "R", false, 0, "")
	setColor(pdf, pdfBandDim)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetX(rightX)
	pdf.CellFormat(rightWidth, 5, latin1(brand.SupportEmail), "", 1, "R", false, 0, "")
}

// drawBand fills a full-width horizontal band in the dark brand colour.
func drawBand(pdf *gofpdf.Fpdf, top, height float64) {
	pdf.SetFillColor(pdfBand[0], pdfBand[1], pdfBand[2])
	pdf.Rect(0, top, pageWidth, height, "F")
}

// drawBrandMark places the logo if one is configured and usable, and otherwise
// writes the site name as a text wordmark.
//
// A decorative asset must never fail a delivery, so every failure path here is a
// fallback rather than an error (research R-005). The file is read and validated
// before gofpdf sees it, because gofpdf latches an error state that would then
// fail the whole document.
func drawBrandMark(pdf *gofpdf.Fpdf, brand Branding, right, bandTop, bandHeight float64) {
	if content := logoPNG(brand); len(content) > 0 {
		const imageType = "PNG"
		name := "brand-logo"
		info := pdf.GetImageInfo(name)
		if info == nil {
			info = pdf.RegisterImageOptionsReader(name,
				gofpdf.ImageOptions{ImageType: imageType}, bytes.NewReader(content))
		}
		if pdf.Ok() && info != nil && info.Height() > 0 {
			// Fit to the BAND, not to an arbitrary width. The asset's background
			// is opaque, so anything taller than the band draws a dark rectangle
			// hanging past it — which is exactly what a width-driven auto height
			// produced. Height is the constraint; width follows from the asset's
			// own aspect ratio, so replacing the asset cannot break the layout.
			height := bandHeight - 2*logoBandInset
			width := height * info.Width() / info.Height()
			pdf.ImageOptions(name, right-width, bandTop+logoBandInset, width, height, false,
				gofpdf.ImageOptions{ImageType: imageType}, 0, "")
			if pdf.Ok() {
				return
			}
			// Registration succeeded but placement did not. Clear the latched
			// error so the rest of the document still renders, and fall through
			// to the wordmark.
			pdf.SetError(nil)
		}
	}

	setColor(pdf, pdfBandFg)
	pdf.SetFont("Helvetica", "B", 15)
	pdf.SetXY(right-42, bandTop+(bandHeight-10)/2)
	pdf.CellFormat(42, 10, latin1(strings.ToUpper(brand.SiteName)), "", 0, "R", false, 0, "")
}

// logoBandInset is the breathing room above and below the mark inside its band.
const logoBandInset = 3.5

func setColor(pdf *gofpdf.Fpdf, c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }

// brandOr falls back when a branding string was configured empty.
func brandOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
