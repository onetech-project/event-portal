// Package notification renders ticket PDFs and delivers them by email. It owns no
// database tables — it reaches the order and ticket domains through the narrow
// interfaces declared in service.go (Constitution Principle II).
package notification

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/jung-kurt/gofpdf"
	qrcode "github.com/skip2/go-qrcode"
)

// qrPixelSize is the rendered PNG's edge length. It is generous enough that the
// image stays sharp when the PDF is printed rather than scanned from a screen.
const qrPixelSize = 512

// TicketDetail is everything printed on one ticket.
type TicketDetail struct {
	TicketCode     string
	AttendeeName   string
	TicketTypeName string
	EventName      string
	Venue          string
	StartDate      time.Time
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

func renderTicketPage(pdf *gofpdf.Fpdf, order OrderDelivery, ticket TicketDetail, index int) error {
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 20)
	pdf.CellFormat(0, 12, ticket.EventName, "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(0, 7, ticket.Venue, "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 7, ticket.StartDate.Format("Mon, 02 Jan 2006 15:04 MST"), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetDrawColor(200, 200, 200)
	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())
	pdf.Ln(6)

	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(0, 8, ticket.AttendeeName, "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(0, 7, "Ticket type: "+ticket.TicketTypeName, "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 7, "Order: "+order.OrderNumber, "", 1, "L", false, 0, "")
	pdf.Ln(6)

	png, err := RenderQR(ticket.TicketCode)
	if err != nil {
		return err
	}

	// A distinct image name per page: gofpdf caches registered images by name, so
	// reusing one name would print the first ticket's QR on every page.
	imageName := fmt.Sprintf("qr-%d-%s", index, ticket.TicketCode)
	pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	pdf.ImageOptions(imageName, 10, pdf.GetY(), 60, 60, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")

	pdf.SetY(pdf.GetY() + 64)
	pdf.SetFont("Courier", "B", 18)
	pdf.CellFormat(0, 10, ticket.TicketCode, "", 1, "L", false, 0, "")

	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(110, 110, 110)
	pdf.MultiCell(0, 5,
		"Show this QR code or read out the ticket code at the entrance. "+
			"Each ticket admits one person and can only be used once.", "", "L", false)
	pdf.SetTextColor(0, 0, 0)

	return pdf.Error()
}
