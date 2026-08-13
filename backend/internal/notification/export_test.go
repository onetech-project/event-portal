package notification

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
