package notification

// Branding is the platform's own identity, printed on the email body and on both
// of its attachments (spec 016 FR-035).
//
// It is platform-WIDE, deliberately: FR-036 forbids storing branding against an
// event, so every order's documents carry these same strings whichever event was
// bought. Only the event's own recorded name, venue and address vary per order.
//
// It is injected rather than read from config inside the renderers, so the render
// functions stay pure and testable — the same arrangement SMTPConfig already has.
type Branding struct {
	// SiteName is the wordmark: the e-ticket footer's left column and, as text,
	// the header band of the email and both PDFs.
	SiteName string
	// SiteURL is printed under SiteName on the e-ticket footer.
	SiteURL string
	// SupportEmail is the customer-service address on the receipt and e-ticket
	// footers. It is the one string here most likely to change without an
	// engineer available, which is why none of these are constants.
	SupportEmail string
	// LegalEntity is the processor named in the receipt's closing statement.
	LegalEntity string
	// Attribution closes the RECEIPT: "Powered By Manjo". It must not appear in
	// the email body — the design gives the two documents different closings, and
	// FR-029 fixes the email's wording exactly.
	Attribution string
	// Copyright closes the EMAIL BODY: "© 2026 manjo" (FR-029).
	Copyright string
	// LogoPath and PinPath OVERRIDE the assets compiled into the binary. Empty —
	// the normal case — uses the embedded ones (see assets.go). A path that
	// cannot be read falls back to the embedded asset rather than to nothing, so
	// a typo in an environment variable cannot strip the brand mark from every
	// document.
	//
	// Since spec 016 the logo is a requirement rather than an ornament
	// (FR-023a): the email carries it as an inline image part referenced by
	// content ID, and both PDFs embed it directly. The text wordmark survives
	// only as the failure path — shipping on it does not satisfy FR-023a.
	//
	// The email still never fetches a REMOTE image: mainstream clients block
	// those by default, and a remote fetch from a transactional receipt is
	// shaped exactly like a tracking pixel.
	LogoPath string
	PinPath  string
}
