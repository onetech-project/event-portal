// Package notification renders ticket PDFs and delivers them by email. It owns no
// database tables — it reaches the order and ticket domains through the narrow
// interfaces declared in service.go (Constitution Principle II).
package notification

import (
	"bytes"
	"errors"
	"fmt"
	"math"
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
	pdfInk  = [3]int{24, 24, 27}
	pdfLine = [3]int{224, 224, 228}
	pdfGray = [3]int{110, 110, 110}
	// #151A26, the design's band colour.
	//
	// This used to be load-bearing for a second reason: the retired mark had a
	// fully opaque background of exactly this colour, so any other value drew a
	// visible rectangle around it. The mark shipped since 2026-08-19 has a real
	// alpha channel (research R-020), so that constraint is gone and this is a
	// free design choice again. Recorded because the old comment would otherwise
	// have a future reader preserve a coupling that no longer exists.
	pdfBand   = [3]int{21, 26, 38}
	pdfBandFg = [3]int{236, 238, 243}
	// The footer's VALUES — the site address and the support address — in pure
	// white, so the one thing a buyer acts on is the brightest text in the band
	// (FR-022f). This inverts what the code did until 2026-08-19.
	pdfBandValue = [3]int{255, 255, 255}
	// The accent rule beside the QR panel: #334155, the design's slate. Not a
	// brand accent — this is structure.
	pdfAccent = [3]int{51, 65, 85}
	// #475569, the event name above the ticket-type heading (FR-019b).
	//
	// Distinct from pdfAccent on purpose. FR-022b reserves #334155 for the QR
	// rule, and the design draws these two slates at different weights for
	// different jobs — collapsing them would lose that.
	pdfEventName = [3]int{71, 85, 105}
	// #d0d1d4, the e-ticket footer's section labels (FR-022f).
	//
	// The design expresses this as white at 80% opacity over the band. A PDF has
	// no alpha here, so the value is the flattened composite:
	// 0.8*255 + 0.2*band, componentwise against #151a26.
	pdfBandLabel = [3]int{208, 209, 212}
)

// A4 portrait geometry, in mm. Named because three renderers share them and a
// stray literal is how two documents drift apart.
const (
	pageWidth   = 210.0
	pageHeight  = 297.0
	marginLeft  = 17.0
	marginRight = 17.0
	contentWide = pageWidth - marginLeft - marginRight

	// The band is FIXED and the mark is fitted into it — the mark never sizes the
	// band (FR-023b, reversed 2026-08-19).
	//
	// A 38.3mm band was tried, to make the lockup's secondary line legible in
	// print, and rejected: it pushed the whole page down and made the document
	// visibly taller for a credit nobody needs to read off a ticket. The mark is
	// capped by height instead, so replacing the asset can never grow the page.
	bandHeight = 21.0
)

// The e-ticket page is absolutely positioned — SetAutoPageBreak is off, so a tall
// event name cannot silently spill one ticket across two pages. The cost used to
// be that every y below the band was a literal, so changing the band's height
// meant editing each by hand and hoping none was missed (research R-022).
//
// They are offsets from bandHeight now, so the band and the page move together.
// A band moved without the accent rule draws a 72mm rule straight through it.
const (
	headingY = bandHeight + 8   // "Ticket N of M"
	accentY  = bandHeight + 19  // the accent rule beside the QR panel
	accentH  = 72.0
	qrX      = 22.0
	qrY      = bandHeight + 25
	qrSize   = 62.0
	eventY   = bandHeight + 111 // event name above the ticket-type headline
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
	pdf.SetXY(marginLeft, headingY)
	pdf.CellFormat(contentWide, 9, latin1(fmt.Sprintf("Ticket %d of %d", index+1, total)), "", 1, "L", false, 0, "")

	// The accent bar down the left edge, alongside the identity block. It sits
	// FLUSH against the page edge, so only its right corners are rounded — "23"
	// is top-right and bottom-right. Rounding the left pair would round corners
	// nobody can see and leave the bar looking detached from the edge.
	pdf.SetFillColor(pdfAccent[0], pdfAccent[1], pdfAccent[2])
	pdf.RoundedRect(0, accentY, 3.0, accentH, 1.5, "23", "F")

	// QR panel, left.
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

	// Event name above the ticket-type headline (FR-019), in the design's slate
	// rather than the brand crimson it used until 2026-08-19 (FR-019b).
	setColor(pdf, pdfEventName)
	pdf.SetFont("Helvetica", "B", 9.5)
	pdf.SetXY(marginLeft, eventY)
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
// customer-service block on the right (FR-022, FR-022a, FR-022d).
//
// The two blocks sit at opposite edges of the band. Within the right-hand block
// the label, the envelope and the address share ONE left edge — see
// customerServiceBlock for why that replaced right-aligning each line.
//
// The geometry is computed by customerServiceBlock rather than inline, so the
// arithmetic is testable without extracting positions from compressed PDF bytes.
// What remains here is a thin mapping from those numbers to draw calls.
func drawTicketFooter(pdf *gofpdf.Fpdf, brand Branding) {
	const footerTop = pageHeight - 25
	drawBand(pdf, footerTop, 25)

	// Cell margin OFF for this band, restored before returning.
	//
	// gofpdf insets left-aligned cell text by cMargin (fpdf.go:2396), which
	// defaults to one tenth of the page margin — 1mm here. Vector primitives get
	// no such inset, so an envelope drawn at the same x as its label sits 1mm to
	// its LEFT and the two can never share an edge, however carefully the
	// arithmetic is done. FR-022a asks for exactly one shared edge, so the inset
	// is removed rather than compensated for at each call site.
	//
	// Sticky state, like the cap and join styles in drawEnvelope: restore it or
	// every later cell on the page loses its padding.
	cellMargin := pdf.GetCellMargin()
	pdf.SetCellMargin(0)
	defer pdf.SetCellMargin(cellMargin)

	const labelY, valueY = 7.0, 11.5

	setColor(pdf, pdfBandLabel)
	pdf.SetFont("Helvetica", "B", 7.5)
	pdf.SetXY(marginLeft, footerTop+labelY)
	pdf.CellFormat(80, 4, latin1(strings.ToUpper(brand.SiteName)), "", 1, "L", false, 0, "")
	setColor(pdf, pdfBandValue)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetX(marginLeft)
	pdf.CellFormat(80, 5, latin1(brand.SiteURL), "", 1, "L", false, 0, "")

	const label = "CUSTOMER SERVICE"
	blockLeft, blockWidth := customerServiceBlock(pdf, label, brand.SupportEmail)

	setColor(pdf, pdfBandLabel)
	pdf.SetFont("Helvetica", "B", 7.5)
	pdf.SetXY(blockLeft, footerTop+labelY)
	pdf.CellFormat(blockWidth, 4, label, "", 1, "L", false, 0, "")

	// The envelope, then the address on the SAME line (FR-022d).
	//
	// textY is captured BEFORE drawing the icon, for the reason FR-015b already
	// records on the receipt: gofpdf's MoveTo sets the current position, so
	// reading GetY() after drawEnvelope returns the icon path's own Y and pushes
	// the address below its own icon.
	//
	// The body is white, matching the address it precedes (FR-022f). The FLAP
	// stays the band colour rather than becoming white too: on this ground that
	// is what makes the envelope read as a cut-out instead of a filled block.
	const lineHeight = 5.0
	const bodyTop, bodyBottom = 2.0 / 12, 10.0 / 12
	textY := footerTop + valueY
	iconY := textY + lineHeight/2 - footerIconSize*(bodyTop+bodyBottom)/2
	drawEnvelope(pdf, blockLeft, iconY, footerIconSize, pdfBandValue, pdfBand)

	setColor(pdf, pdfBandValue)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(blockLeft+footerIconSize+footerIconGap, textY)
	pdf.CellFormat(blockWidth-footerIconSize-footerIconGap, lineHeight,
		latin1(brand.SupportEmail), "", 1, "L", false, 0, "")
}

// Footer icon geometry. The envelope's box and the gap between it and the
// address it precedes (FR-022d).
const (
	footerIconSize = 3.2
	footerIconGap  = 1.6
)

// customerServiceBlock reports where the footer's customer-service block starts
// and how wide it is.
//
// The block is right-POSITIONED — its right edge meets the right margin — but its
// two lines are LEFT-aligned with one another, so the label, the envelope and the
// address share one starting edge (FR-022a). That is what frame 683:247 draws:
// the block sits at x=432 of a 595pt page with both children at x=0.
//
// The design's mock happens to show the address line ending flush at the right
// margin, but that is a coincidence of that particular address's length, not a
// rule. Right-aligning each line independently — which is what this replaced —
// would slide the envelope horizontally with every change of address length and
// break its alignment under the label (FR-022e).
func customerServiceBlock(pdf *gofpdf.Fpdf, label, address string) (left, width float64) {
	pdf.SetFont("Helvetica", "B", 7.5)
	labelWidth := pdf.GetStringWidth(latin1(label))

	pdf.SetFont("Helvetica", "", 9)
	addressWidth := footerIconSize + footerIconGap + pdf.GetStringWidth(latin1(address))

	width = math.Max(labelWidth, addressWidth)
	return pageWidth - marginRight - width, width
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
