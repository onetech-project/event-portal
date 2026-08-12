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

// TicketDetail is everything printed on one ticket. AttendeeEmail is not
// printed — it is the per-holder delivery address the sender groups by
// (spec 011).
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

// FormatTicketWindow renders a ticket's admission window for the printed ticket
// and the email.
//
// A window that opens and closes on the same day — the ordinary case — reads as
// one date with a time range rather than as two near-identical datetimes, which
// is what a holder glancing at a ticket at the gate actually needs.
func FormatTicketWindow(start, end time.Time) string {
	const dateTime = "Mon, 02 Jan 2006 15:04 MST"
	if end.IsZero() || end.Equal(start) {
		return start.Format(dateTime)
	}
	sy, sm, sd := start.Date()
	ey, em, ed := end.Date()
	if sy == ey && sm == em && sd == ed {
		return fmt.Sprintf("%s - %s", start.Format(dateTime), end.Format("15:04 MST"))
	}
	return fmt.Sprintf("%s - %s", start.Format(dateTime), end.Format(dateTime))
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
func RenderTicketsPDF(order OrderDelivery, tickets []TicketDetail) ([]byte, error) {
	if len(tickets) == 0 {
		return nil, errors.New("notification: cannot render a ticket PDF with no tickets")
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("E-Ticket "+order.OrderNumber, true)
	pdf.SetAuthor("Event Ticketing", true)

	for i, ticket := range tickets {
		if err := renderTicketPage(pdf, order, ticket, i); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("write ticket PDF: %w", err)
	}
	return buf.Bytes(), nil
}

// The brand palette from the frontend tokens, so the printed ticket matches the
// site and the email (Figma 251-2).
var (
	pdfBrand   = [3]int{203, 28, 79}
	pdfSurface = [3]int{251, 239, 243}
	pdfLine    = [3]int{236, 171, 191}
	pdfGray    = [3]int{110, 110, 110}
)

// renderTicketPage draws one e-ticket in the Figma 251-2 card layout: a header
// with the order number, a brand-tinted card holding the ticket-type chip, event
// details and attendee on the left and the QR panel on the right, and the entry
// instructions underneath.
func renderTicketPage(pdf *gofpdf.Fpdf, order OrderDelivery, ticket TicketDetail, index int) error {
	pdf.AddPage()

	// Header: "E-Ticket" wordmark and the order number it belongs to.
	pdf.SetTextColor(pdfBrand[0], pdfBrand[1], pdfBrand[2])
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(95, 10, "E-Ticket", "", 0, "L", false, 0, "")
	pdf.SetTextColor(pdfGray[0], pdfGray[1], pdfGray[2])
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(95, 10, "Inv: #"+order.OrderNumber, "", 1, "R", false, 0, "")
	pdf.SetDrawColor(pdfBrand[0], pdfBrand[1], pdfBrand[2])
	pdf.SetLineWidth(0.6)
	pdf.Line(10, pdf.GetY()+1, 200, pdf.GetY()+1)

	// The ticket card.
	const cardTop, cardHeight = 32.0, 74.0
	pdf.SetFillColor(pdfSurface[0], pdfSurface[1], pdfSurface[2])
	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.SetLineWidth(0.3)
	pdf.Rect(10, cardTop, 190, cardHeight, "FD")

	// Ticket-type chip.
	pdf.SetFont("Helvetica", "B", 9)
	chipLabel := strings.ToUpper(ticket.TicketTypeName)
	chipWidth := pdf.GetStringWidth(chipLabel) + 8
	pdf.SetFillColor(pdfBrand[0], pdfBrand[1], pdfBrand[2])
	pdf.Rect(16, cardTop+6, chipWidth, 7, "F")
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(16, cardTop+6)
	pdf.CellFormat(chipWidth, 7, chipLabel, "", 1, "C", false, 0, "")

	// Event name + detail rows, left of the QR panel.
	const detailWidth = 116.0
	pdf.SetTextColor(24, 24, 27)
	pdf.SetFont("Helvetica", "B", 18)
	pdf.SetXY(16, cardTop+16)
	pdf.MultiCell(detailWidth, 8, ticket.EventName, "", "L", false)

	labelValue := func(label, value string) {
		pdf.SetX(16)
		pdf.SetTextColor(pdfGray[0], pdfGray[1], pdfGray[2])
		pdf.SetFont("Helvetica", "B", 7.5)
		pdf.CellFormat(detailWidth, 4, label, "", 1, "L", false, 0, "")
		pdf.SetX(16)
		pdf.SetTextColor(24, 24, 27)
		pdf.SetFont("Helvetica", "", 11)
		pdf.MultiCell(detailWidth, 5.5, value, "", "L", false)
		pdf.Ln(1.5)
	}
	labelValue("DATE & TIME", FormatTicketWindow(ticket.EventStart, ticket.EventEnd))
	labelValue("VENUE", ticket.Venue)

	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.Line(16, pdf.GetY(), 16+detailWidth, pdf.GetY())
	pdf.Ln(2)
	labelValue("ATTENDEE", ticket.AttendeeName)

	// QR panel: white box on the card's right, code underneath.
	const qrBoxX, qrBoxWidth = 140.0, 54.0
	pdf.SetFillColor(255, 255, 255)
	pdf.SetDrawColor(pdfLine[0], pdfLine[1], pdfLine[2])
	pdf.Rect(qrBoxX, cardTop+8, qrBoxWidth, cardHeight-16, "FD")

	png, err := RenderQR(ticket.TicketCode)
	if err != nil {
		return err
	}

	// A distinct image name per page: gofpdf caches registered images by name, so
	// reusing one name would print the first ticket's QR on every page.
	imageName := fmt.Sprintf("qr-%d-%s", index, ticket.TicketCode)
	pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	pdf.ImageOptions(imageName, qrBoxX+7, cardTop+12, 40, 40, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")

	pdf.SetXY(qrBoxX, cardTop+54)
	pdf.SetTextColor(pdfGray[0], pdfGray[1], pdfGray[2])
	pdf.SetFont("Helvetica", "B", 6.5)
	pdf.CellFormat(qrBoxWidth, 4, "SHOW AT ENTRY", "", 1, "C", false, 0, "")
	pdf.SetX(qrBoxX)
	pdf.SetTextColor(24, 24, 27)
	pdf.SetFont("Courier", "B", 12)
	pdf.CellFormat(qrBoxWidth, 6, ticket.TicketCode, "", 1, "C", false, 0, "")

	// Entry instructions + legal line under the card.
	pdf.SetY(cardTop + cardHeight + 6)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(pdfGray[0], pdfGray[1], pdfGray[2])
	pdf.MultiCell(0, 5,
		"Show this QR code or read out the ticket code at the entrance. "+
			"Each ticket admits one person and can only be used once.", "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "", 8)
	pdf.MultiCell(0, 4,
		"This is a valid proof of payment and your official e-ticket. No signature is required.",
		"", "L", false)
	pdf.SetTextColor(0, 0, 0)

	return pdf.Error()
}
