package notification

import (
	"bytes"
	"fmt"
	"math"

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

// --- Spec 016 Revision 5: colour seams -------------------------------------

// ColorOperator returns the PDF content-stream operator gofpdf emits for a text
// or fill colour, so a test can search a rendered page for the colour a node was
// ACTUALLY drawn in.
//
// This exists because the alternative is circular: a test that compares a
// package constant against itself passes for any value, which is the blind spot
// research R-027 found in the mask tests. Here the expected operator is built
// from a literal in the test and matched against the document's own bytes.
//
// The formatting mirrors gofpdf's rgbColorValue (fpdf.go:863) exactly, quirk
// included: a colour it considers grey collapses to the one-component "g"
// operator instead of three components and "rg". Its grey test compares the red
// and green INTEGER components but the red and blue FLOAT ones; that asymmetry
// is reproduced rather than corrected, because the goal is to match what the
// library writes, not what it ought to write.
func ColorOperator(c [3]int) string {
	r := float64(c[0]) / 255.0
	g := float64(c[1]) / 255.0
	b := float64(c[2]) / 255.0
	if c[0] == c[1] && r == b {
		return fmt.Sprintf("%.3f g", r)
	}
	return fmt.Sprintf("%.3f %.3f %.3f rg", r, g, b)
}

// The palette values the colour assertions name. Exported individually rather
// than as one map so a deleted constant is a compile error in this file, which
// is where a reader looks to find out what the document is supposed to use.
var (
	PdfEventName = pdfEventName
	PdfBandLabel = pdfBandLabel
	PdfBandFg    = pdfBandFg
	PdfWhite     = [3]int{255, 255, 255}
)

// RetiredCrimson is the brand crimson the e-ticket used for its event name until
// FR-019b moved it to slate.
//
// It is a LITERAL and must stay one. Writing it as pdfBrand would delete this
// guard along with the constant, and SC-024's whole job is to fail if the colour
// ever comes back.
var RetiredCrimson = [3]int{203, 28, 79}

// RelativeLuminance is the WCAG relative luminance of a colour, used to assert
// that the footer's labels recede behind the values they head (FR-022f) rather
// than only that two particular hex values were drawn.
func RelativeLuminance(c [3]int) float64 {
	lin := func(v int) float64 {
		s := float64(v) / 255.0
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}
