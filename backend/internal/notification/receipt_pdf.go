package notification

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// Receipt-specific palette entries. The status word is the only colour on the
// page besides the table header, and it is green because "Paid" is the whole
// point of the document.
var (
	pdfPaid       = [3]int{21, 128, 61}
	pdfTableHead  = [3]int{17, 17, 20}
	pdfTablePanel = [3]int{250, 250, 251}
	// The design's envelope is slate, not the neutral pdfGray used for label text.
	pdfSlate = [3]int{107, 114, 128}
)

// Receipt column geometry, in mm. The product column is deliberately the widest:
// a ticket type's name plus its admission sub-line is the only cell that wraps.
const (
	receiptTop = 20.0
	// colInset is the breathing room between the table's outer edges and its
	// content, applied SYMMETRICALLY. The first cut inset the product column by
	// 5 mm and then right-aligned the amounts flush to the table's own right
	// edge, which read as a visibly lopsided table (FR-011b).
	colInset = 5.0
	// Right edges of the numeric columns; each is rendered right-aligned to it.
	colPriceRight = 132.0
	colQtyRight   = 158.0
	// colTableRight is the table's outer EDGE — where the rules and the panel
	// end. colTotalRight is where the Total column's text right-aligns, one
	// inset in from it. Keeping the two apart is what stops the amounts from
	// touching the border.
	colTableRight = pageWidth - marginRight
	colTotalRight = colTableRight - colInset
	colProductX   = marginLeft + colInset
	colProductW   = colPriceRight - colProductX - 22
	// The totals block is inset from the left so it reads as a summary of the
	// table above rather than as another product row.
	totalsLabelX = 88.0
)

// RenderReceiptPDF builds the Payment Receipt attachment (Figma 688-671): a
// self-contained proof of payment for the whole order.
//
// It is self-contained on purpose (SC-004): a buyer filing this with an expense
// claim, or forwarding it to a finance team, must not have to also forward the
// email it arrived in. Order number, buyer, lines, fees, total and issuer contact
// are all on the document itself.
//
// It carries no ticket code and no QR — money here, admission on the e-ticket.
func RenderReceiptPDF(order OrderDelivery, brand Branding) ([]byte, error) {
	return renderReceiptPDF(order, brand, true)
}

// renderReceiptPDF carries the compress flag the export_test.go seam needs.
func renderReceiptPDF(order OrderDelivery, brand Branding, compress bool) ([]byte, error) {
	if order.OrderNumber == "" {
		return nil, fmt.Errorf("notification: cannot render a receipt with no order number")
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(compress)
	pdf.SetTitle("Payment Receipt "+order.OrderNumber, true)
	pdf.SetAuthor(brandOr(brand.LegalEntity, "Event Ticketing"), true)

	// Unlike the e-ticket, the receipt MAY legitimately run to a second page: an
	// order with many lines has many rows, and truncating a financial document to
	// fit is the one thing it must never do. The break margin leaves room for the
	// attribution, which is drawn by the footer function so it sits on every page
	// without being what triggers the break.
	pdf.SetAutoPageBreak(true, 24)
	pdf.SetFooterFunc(func() {
		setColor(pdf, pdfGray)
		pdf.SetFont("Helvetica", "I", 8.5)
		pdf.SetXY(marginLeft, pageHeight-16)
		pdf.CellFormat(contentWide, 5, latin1(brand.Attribution), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()

	receiptHeader(pdf, order)
	receiptDetailColumns(pdf, order)
	tableBottom := receiptTable(pdf, order)
	receiptFooter(pdf, brand, tableBottom)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("write receipt PDF: %w", err)
	}
	return buf.Bytes(), nil
}

// receiptHeader draws the title, the order number, and the last-updated stamp
// (FR-008).
func receiptHeader(pdf *gofpdf.Fpdf, order OrderDelivery) {
	setColor(pdf, pdfInk)
	pdf.SetFont("Helvetica", "B", 17)
	pdf.SetXY(marginLeft, receiptTop)
	pdf.CellFormat(contentWide, 9, "Payment Receipt", "", 1, "L", false, 0, "")

	setColor(pdf, pdfGray)
	pdf.SetFont("Helvetica", "", 9.5)
	pdf.SetX(marginLeft)
	pdf.CellFormat(contentWide, 5.5, latin1("Order No. : "+order.OrderNumber), "", 1, "L", false, 0, "")
	pdf.SetX(marginLeft)
	pdf.CellFormat(contentWide, 5.5,
		latin1("Last updated : "+formatReceiptStamp(order.UpdatedAt)), "", 1, "L", false, 0, "")
}

// receiptDetailColumns draws the Order Details / Transaction Details pair
// (FR-009, FR-010).
//
// The buyer's email and phone are MASKED here, under the same rule the email body
// uses, so the two surfaces cannot disagree about who bought the order (FR-032,
// FR-033). The name is not masked: it is what makes the receipt identifiable as
// yours.
func receiptDetailColumns(pdf *gofpdf.Fpdf, order OrderDelivery) {
	const top = 47.0
	const rightX = 110.0
	leftWidth := rightX - marginLeft - 10
	rightWidth := pageWidth - marginRight - rightX

	heading := func(x float64, width float64, text string) {
		setColor(pdf, pdfInk)
		pdf.SetFont("Helvetica", "B", 10.5)
		pdf.SetXY(x, top)
		pdf.CellFormat(width, 6, text, "", 1, "L", false, 0, "")
		pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
		pdf.SetLineWidth(0.3)
		pdf.Line(x, top+7, x+width, top+7)
	}
	heading(marginLeft, leftWidth, "Order Details")
	heading(rightX, rightWidth, "Transaction Details")

	// row draws one "Label : value" pair and returns the next y.
	row := func(x, y, width float64, label, value string, valueColor [3]int, bold bool) float64 {
		if value == "" {
			// An unrecorded value omits its row entirely rather than printing a
			// blank or a placeholder (FR-027).
			return y
		}
		setColor(pdf, pdfGray)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetXY(x, y)
		pdf.CellFormat(30, 5.5, latin1(label), "", 0, "L", false, 0, "")
		pdf.CellFormat(4, 5.5, ":", "", 0, "L", false, 0, "")

		setColor(pdf, valueColor)
		style := ""
		if bold {
			style = "B"
		}
		pdf.SetFont("Helvetica", style, 9)
		pdf.SetX(x + 34)
		pdf.MultiCell(width-34, 5.5, latin1(value), "", "L", false)
		return pdf.GetY() + 1
	}

	y := top + 12
	y = row(marginLeft, y, leftWidth, "Customer Name", order.BuyerName, pdfInk, false)
	y = row(marginLeft, y, leftWidth, "Phone Number", MaskPhone(order.BuyerPhone), pdfInk, false)
	_ = row(marginLeft, y, leftWidth, "Email", MaskEmail(order.BuyerEmail), pdfInk, false)

	y = top + 12
	y = row(rightX, y, rightWidth, "Status", brandOr(order.Payment.Status, "Paid"), pdfPaid, true)
	y = row(rightX, y, rightWidth, "Payment Method", strings.ToUpper(order.Payment.Method), pdfInk, false)
	_ = row(rightX, y, rightWidth, "Date", formatTransactionStamp(order.Payment.PaidAt), pdfInk, false)
}

// receiptTable draws the product table and the totals block, returning the y the
// footer may start from (FR-011, FR-012, FR-013, FR-014).
func receiptTable(pdf *gofpdf.Fpdf, order OrderDelivery) float64 {
	top := 92.0

	// Dark header row.
	pdf.SetFillColor(pdfTableHead[0], pdfTableHead[1], pdfTableHead[2])
	pdf.Rect(marginLeft, top, contentWide, 9, "F")
	setColor(pdf, pdfBandFg)
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetXY(colProductX, top)
	pdf.CellFormat(colProductW, 9, "Product", "", 0, "L", false, 0, "")
	// Centred, unlike the other numeric headers (FR-011c). The price values are
	// wide enough to fill most of their column, so a right-aligned header sat
	// visibly off to one side of the figures beneath it. Qty and Total keep right
	// alignment: their values are short and hug the same edge the header does.
	pdf.SetXY(colPriceRight-25, top)
	pdf.CellFormat(25, 9, "Price", "", 0, "C", false, 0, "")
	pdf.SetXY(colQtyRight-20, top)
	pdf.CellFormat(20, 9, "Qty", "", 0, "R", false, 0, "")
	pdf.SetXY(colTotalRight-30, top)
	pdf.CellFormat(30, 9, "Total", "", 0, "R", false, 0, "")

	y := top + 9

	// The panel behind the rows, drawn first so the text sits on it.
	panelStart := y

	for _, line := range order.Items {
		rowTop := y + 4

		setColor(pdf, pdfInk)
		pdf.SetFont("Helvetica", "B", 9.5)
		pdf.SetXY(colProductX, rowTop)
		pdf.MultiCell(colProductW, 5, latin1(line.Name), "", "L", false)
		nameBottom := pdf.GetY()

		// Sub-line: the line's OWN admission day(s) and its description. A bundle
		// lists each day rather than collapsing to the earliest — collapsing is
		// the exact defect spec 015 removed, and re-introducing it here would
		// undo that work in a new place (FR-011).
		if sub := receiptSubLine(line); sub != "" {
			setColor(pdf, pdfGray)
			pdf.SetFont("Helvetica", "", 7.5)
			pdf.SetX(colProductX)
			pdf.MultiCell(colProductW, 4.5, latin1(sub), "", "L", false)
			nameBottom = pdf.GetY()
		}

		setColor(pdf, pdfInk)
		pdf.SetFont("Helvetica", "", 9.5)
		pdf.SetXY(colPriceRight-25, rowTop)
		pdf.CellFormat(25, 5, formatIDR(line.UnitPrice), "", 0, "R", false, 0, "")
		pdf.SetXY(colQtyRight-20, rowTop)
		pdf.CellFormat(20, 5, fmt.Sprintf("x %d", line.Quantity), "", 0, "R", false, 0, "")
		pdf.SetXY(colTotalRight-30, rowTop)
		pdf.CellFormat(30, 5, formatIDR(line.Subtotal), "", 0, "R", false, 0, "")

		y = nameBottom + 4
		pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
		pdf.SetLineWidth(0.2)
		// Flush to BOTH outer edges, not inset from colProductX: an inset rule
		// leaves a visible notch where it meets the table's left border.
		pdf.Line(marginLeft, y, colTableRight, y)
	}

	y = receiptTotals(pdf, order, y+6)

	// The panel outline, now that the extent is known.
	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.SetLineWidth(0.3)
	pdf.SetFillColor(pdfTablePanel[0], pdfTablePanel[1], pdfTablePanel[2])
	pdf.Rect(marginLeft, panelStart, contentWide, y-panelStart, "D")

	return y
}

// receiptTotals draws the subtotal, the frozen fee lines, and the grand total,
// returning the y below them.
//
// A pre-fee order — one placed before order fees existed, so no subtotal was
// recorded — shows the total ALONE. No subtotal row and no invented fee rows
// (FR-013): a receipt that fabricates a breakdown it does not have is worse than
// one that shows only what it knows.
func receiptTotals(pdf *gofpdf.Fpdf, order OrderDelivery, top float64) float64 {
	y := top
	labelWidth := colTotalRight - 30 - totalsLabelX

	line := func(label, value string, bold bool, color [3]int) {
		style := ""
		size := 9.5
		if bold {
			style, size = "B", 10.5
		}
		setColor(pdf, color)
		pdf.SetFont("Helvetica", style, size)
		pdf.SetXY(totalsLabelX, y)
		pdf.CellFormat(labelWidth, 6, latin1(label), "", 0, "L", false, 0, "")
		pdf.SetXY(colTotalRight-30, y)
		pdf.CellFormat(30, 6, value, "", 1, "R", false, 0, "")
		y = pdf.GetY() + 1.5
	}

	if order.Subtotal != nil {
		line("Subtotal", formatIDR(*order.Subtotal), false, pdfInk)

		for _, fee := range order.Fees {
			line(fee.Name, formatIDR(fee.Amount), false, pdfInk)
			// A percentage fee carries the applicable-rates note the design
			// specifies, so a buyer querying the figure knows it is derived
			// rather than fixed (FR-012).
			if strings.Contains(fee.Name, "%") {
				setColor(pdf, pdfGray)
				pdf.SetFont("Helvetica", "", 6.5)
				pdf.SetXY(totalsLabelX, y-1)
				pdf.CellFormat(labelWidth, 4, "*Based on applicable tax rates", "", 1, "L", false, 0, "")
				y = pdf.GetY() + 1
			}
		}

		pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
		pdf.SetLineWidth(0.3)
		pdf.Line(totalsLabelX, y, colTotalRight, y)
		y += 3
	}

	line("Total", formatIDR(order.TotalAmount), true, pdfInk)
	return y + 3
}

// receiptFooter closes the document with the processing statement, the
// customer-service contact, and the platform attribution (FR-015).
func receiptFooter(pdf *gofpdf.Fpdf, brand Branding, top float64) {
	y := top + 14
	if y > pageHeight-60 {
		y = pageHeight - 60
	}

	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.SetLineWidth(0.3)
	pdf.Line(marginLeft, y, pageWidth-marginRight, y)

	setColor(pdf, pdfGray)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(marginLeft, y+7)
	pdf.MultiCell(contentWide, 5.5, latin1(fmt.Sprintf(
		"This payment receipt is valid and processed through %s's system.", brand.LegalEntity)),
		"", "L", false)
	pdf.SetX(marginLeft)
	pdf.MultiCell(contentWide, 5.5,
		"If you experience any issues or need assistance, please contact our customer service at:",
		"", "L", false)

	// Envelope, then the address on the SAME line (FR-015a, FR-015b).
	//
	// textY is captured BEFORE drawing the icon and is what both the icon and the
	// text are positioned from. gofpdf's MoveTo sets the current position, so
	// reading GetY() after drawEnvelope returns the icon path's own Y — which is
	// what previously pushed the address ~2 mm below its icon.
	const iconSize, iconGap, lineHeight = 3.5, 1.6, 5.5
	textY := pdf.GetY()

	// Centre the icon's BODY on the text's optical centre. gofpdf puts a cell's
	// baseline at y + h/2 + 0.3*fontSize, and the body spans 2u..10u of the box,
	// so the box top is the text centre less half the body height less that 2u.
	const bodyTop, bodyBottom = 2.0 / 12, 10.0 / 12
	textCentre := textY + lineHeight/2
	iconY := textCentre - iconSize*(bodyTop+bodyBottom)/2
	drawEnvelope(pdf, marginLeft, iconY, iconSize)

	setColor(pdf, pdfInk)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(marginLeft+iconSize+iconGap, textY)
	pdf.CellFormat(contentWide-iconSize-iconGap, lineHeight,
		latin1(brand.SupportEmail), "", 1, "L", false, 0, "")

	// The attribution is drawn by the footer function set in renderReceiptPDF, so
	// it lands on every page and never triggers a page break of its own.
}

// drawEnvelope draws the customer-service icon with vector primitives: a filled
// rounded body with a light V-flap (FR-015a).
//
// Vector rather than a font glyph or a PNG, for reasons worth keeping:
// ZapfDingbats (which gofpdf does ship, envelope at 0x29) draws a thin OUTLINE
// envelope with a centre seal, which reads as a different icon beside 9pt text;
// and a raster PNG would put the only bitmap into an otherwise all-vector
// document, along with an asset file and a failure branch.
//
// Choosing vector also satisfies FR-015a's "an icon that cannot be drawn MUST be
// omitted" by construction — there is no asset that can be missing — so no dead
// fallback branch is written.
//
// size is the icon's box in mm; u is one design unit within it.
func drawEnvelope(pdf *gofpdf.Fpdf, x, y, size float64) {
	u := size / 12

	pdf.SetFillColor(pdfSlate[0], pdfSlate[1], pdfSlate[2])
	pdf.RoundedRect(x+1*u, y+2*u, 10*u, 8*u, 1.2*u, "1234", "F")

	// The flap. Round cap and join so the V reads cleanly at this size.
	pdf.SetDrawColor(255, 255, 255)
	pdf.SetLineWidth(1.4 * u)
	pdf.SetLineCapStyle("round")
	pdf.SetLineJoinStyle("round")
	pdf.MoveTo(x+1.8*u, y+3.0*u)
	pdf.LineTo(x+6.0*u, y+6.4*u)
	pdf.LineTo(x+10.2*u, y+3.0*u)
	pdf.DrawPath("D")

	// CRITICAL: cap and join style are sticky Fpdf state. Without this reset
	// every later rule on the page inherits round ends — a defect that passes
	// every content assertion and still looks wrong.
	pdf.SetLineCapStyle("butt")
	pdf.SetLineJoinStyle("miter")
	pdf.SetLineWidth(0.3)
}

// receiptSubLine builds a product row's second line: the day(s) this line admits
// on, then its own description. Either half may be absent; with both absent the
// sub-line is omitted rather than rendering a bare separator.
func receiptSubLine(line ReceiptLine) string {
	parts := make([]string, 0, len(line.AdmissionStarts))
	for _, day := range line.AdmissionStarts {
		if day.IsZero() {
			continue
		}
		// Convert BEFORE taking the calendar date. A ticket admitting at
		// 06:00 WIB is 23:00Z the previous day, so formatting the scanned value
		// prints the wrong DAY — telling a Day 2 holder they admit on Day 1.
		// This is the defect spec 016 Revision 2 exists to fix.
		parts = append(parts, inJakarta(day).Format("02 Jan 2006"))
	}

	dates := strings.Join(parts, ", ")
	descriptor := strings.TrimSpace(line.Descriptor)
	switch {
	case dates != "" && descriptor != "":
		return dates + " • " + descriptor
	case dates != "":
		return dates
	default:
		return descriptor
	}
}

// The three stamps below differ by punctuation alone, which is exactly why they
// are three named functions rather than one with a layout argument: the
// difference then lives in code instead of in a caller's memory.
//
// Every one converts to Jakarta first. Formatting a scanned value directly
// prints whatever zone pgx happened to put it in — UTC in the container, WIB on
// a developer's machine — so the output would be host-dependent.

// formatReceiptStamp renders the receipt header's last-updated time:
// "14:35, 20 Apr 2026 WIB".
//
// It carries the zone even though it is not a payment time: the Date field two
// blocks below says WIB, and one zoneless stamp beside one zoned stamp invites
// the reader to assume they are in different zones.
func formatReceiptStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return inJakarta(t).Format("15:04, 02 Jan 2006 MST")
}

// formatTransactionStamp renders the receipt's settlement time, which is what
// makes it checkable against a bank statement: "20 Apr 2026, 14:30 WIB".
func formatTransactionStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return inJakarta(t).Format("02 Jan 2006, 15:04 MST")
}

// formatOrderStamp renders the email body's order date: "04 Jul 2026 06:56 WIB".
// No comma, unlike the receipt's transaction stamp above.
func formatOrderStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return inJakarta(t).Format("02 Jan 2006 15:04 MST")
}
