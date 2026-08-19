package notification

import (
	"bytes"

	"github.com/jung-kurt/gofpdf"
)

// Test seams. This file compiles only under `go test`, so nothing here widens the
// package's real API.
//
// gofpdf compresses content streams, which means the text a page draws is not
// findable in the shipped bytes. Every assertion about what a document SAYS —
// rather than how big it is — needs the uncompressed variant. The compressed
// path is what production sends; these render the same drawing calls with
// compression off so a test can read them.

// RenderTicketsPDFPlain renders the e-ticket document uncompressed.
func RenderTicketsPDFPlain(order OrderDelivery, tickets []TicketDetail, brand Branding) ([]byte, error) {
	return renderTicketsPDF(order, tickets, brand, false)
}

// RenderReceiptPDFPlain renders the receipt uncompressed.
func RenderReceiptPDFPlain(order OrderDelivery, brand Branding) ([]byte, error) {
	return renderReceiptPDF(order, brand, false)
}

// CidLogo and CidPin expose the content IDs the email body references, so the
// extension guard in smtp_test.go can assert the names actually shipped rather
// than a literal list that drifts silently when a constant changes.
const (
	CidLogo = cidLogo
	CidPin  = cidPin
)

// Latin1 exposes the cp1252 guard so its behaviour can be asserted directly
// rather than only through a rendered page.
func Latin1(s string) string { return latin1(s) }

// BuildEmailBody exposes the body renderer. It is unexported in production
// because SendTicketEmail is the only legitimate caller.
func BuildEmailBody(order OrderDelivery, tickets []TicketDetail, brand Branding) (string, []Attachment) {
	return buildEmailBody(order, tickets, brand)
}

// TableInsets exposes the receipt table's horizontal padding on each side, so a
// test can assert they match without reproducing the geometry (spec 016 FR-011b).
func TableInsets() (left, right float64) {
	return colProductX - marginLeft, colTableRight - colTotalRight
}

// HeaderColumnRights exposes the right edges the Price and Total headers are laid
// out against, so a test can measure their alignment (spec 016 FR-011c).
func HeaderColumnRights() (price, total float64) {
	return colPriceRight, colTotalRight
}

// --- Spec 016 Revision 3: brand refresh seams -----------------------------

// BandHeight exposes the header band's height so a test can assert the page's
// other constants were shifted with it rather than left behind (R-022).
const BandHeight = bandHeight

// TicketFooterGeometry reports the customer-service block's shared left edge and
// its overall width for a given support address, so FR-022a's alignment can be
// asserted without reproducing gofpdf's font metrics in the test.
func TicketFooterGeometry(address string) (left, width float64) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	return customerServiceBlock(pdf, "CUSTOMER SERVICE", address)
}

// FooterRightMargin is the x the block's right edge must meet.
const FooterRightMargin = pageWidth - marginRight

// BrandMarkBox reports the width and height the mark is drawn at inside the
// header band, so a test can assert the band was sized to hold a legible mark
// (FR-023b) rather than the mark squeezed to fit the band.
func BrandMarkBox(brand Branding) (w, h float64, ok bool) {
	content := logoPNG(brand)
	if len(content) == 0 {
		return 0, 0, false
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	info := pdf.RegisterImageOptionsReader("probe",
		gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(content))
	if !pdf.Ok() || info == nil || info.Height() == 0 {
		return 0, 0, false
	}
	h = bandHeight - 2*logoBandInset
	return h * info.Width() / info.Height(), h, true
}
