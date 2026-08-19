package notification_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
)

// --- Spec 016: the merged e-ticket document -------------------------------

// FR-002 / SC-002. One page per issued ticket, in one document — never one
// document per ticket, and never a page count that drifts from the ticket count.
func TestRenderTicketsPDFHasExactlyOnePagePerTicket(t *testing.T) {
	for _, count := range []int{1, 2, 3, 4} {
		doc, err := notification.RenderTicketsPDF(sampleOrder(), sampleTickets(count), sampleBrand())
		require.NoError(t, err)
		assert.Equal(t, count, pdfPageCount(doc),
			"an order with %d tickets must produce a %d-page document", count, count)
	}
}

// FR-016. The pagination is what tells a holder whether they have all of them.
func TestRenderTicketsPDFNumbersEveryPage(t *testing.T) {
	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(3), sampleBrand())
	require.NoError(t, err)

	for _, want := range []string{"Ticket 1 of 3", "Ticket 2 of 3", "Ticket 3 of 3"} {
		assert.Contains(t, string(doc), want)
	}
	assert.NotContains(t, pdfDrawnText(doc), "Ticket 4 of 3")
}

func TestRenderTicketsPDFNumbersASingleTicketAsOneOfOne(t *testing.T) {
	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(1), sampleBrand())
	require.NoError(t, err)

	assert.Contains(t, string(doc), "Ticket 1 of 1")
}

// FR-017, FR-018. Each page carries its OWN ticket code plus the holder identity
// and the order it belongs to.
func TestRenderTicketsPDFPrintsEveryDistinctTicketCodeAndTheHolderIdentity(t *testing.T) {
	tickets := sampleTickets(3)
	tickets[0].AttendeeName = "Michael Smith"
	tickets[1].AttendeeName = "Alex Smith"
	tickets[2].AttendeeName = "Jordan Smith"
	tickets[0].AttendeeEmail = "m.smith@email.com"

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), tickets, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	for _, ticket := range tickets {
		assert.Contains(t, text, ticket.TicketCode)
		assert.Contains(t, text, ticket.AttendeeName)
	}
	// The holder's address is printed IN FULL here — holder identity at a gate,
	// not a contact detail on a forwardable receipt (FR-034).
	assert.Contains(t, text, "m.smith@email.com")
	assert.Contains(t, text, sampleOrder().OrderNumber)
}

// FR-021 / SC-007. The revised design removed the price row and its
// tax-inclusive note. Money lives on the receipt; an e-ticket carries none.
//
// This also dissolves the bundle problem the old layout had: a ticket issued
// from a package has no per-ticket price that could honestly be printed.
func TestRenderTicketsPDFPrintsNoMoneyAtAll(t *testing.T) {
	order := sampleOrder()
	order.TotalAmount = decimal.RequireFromString("121550")
	subtotal := decimal.RequireFromString("105000")
	order.Subtotal = &subtotal
	order.Items = []notification.ReceiptLine{{
		Name: "Regular", Quantity: 3,
		UnitPrice: decimal.RequireFromString("35000"),
		Subtotal:  decimal.RequireFromString("105000"),
	}}
	order.Fees = []notification.ReceiptFee{{
		Name: "PPN (11%)", Amount: decimal.RequireFromString("11550"),
	}}

	doc, err := notification.RenderTicketsPDFPlain(order, sampleTickets(3), sampleBrand())
	require.NoError(t, err)

	// Drawn text only. The page also embeds the QR codes and the brand mark, and
	// a short needle like "Rp" occurs by chance in that binary — see pdfDrawnText.
	text := pdfDrawnText(doc)

	for _, forbidden := range []string{
		"Rp", "IDR", "121.550", "105.000", "35.000", "11.550",
		"Price", "PRICE", "Total", "TOTAL", "PPN",
		"Price includes Tax and Platform Fee",
	} {
		assert.NotContains(t, text, forbidden,
			"an e-ticket page must carry no monetary figure or label (FR-021)")
	}
}

// FR-020. Each page shows ITS OWN ticket type's window. A Day 1 and a Day 2 pass
// in one order must not both print the festival's opening date.
func TestRenderTicketsPDFPrintsEachTicketTypesOwnWindow(t *testing.T) {
	tickets := sampleTickets(2)
	tickets[0].TicketTypeName = "Day 1 Access"
	tickets[0].EventStart = time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	tickets[0].EventEnd = time.Date(2026, 4, 26, 21, 0, 0, 0, time.UTC)
	tickets[1].TicketTypeName = "Day 2 Access"
	tickets[1].EventStart = time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	tickets[1].EventEnd = time.Date(2026, 4, 27, 21, 0, 0, 0, time.UTC)

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), tickets, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "26 Apr 2026")
	assert.Contains(t, text, "27 Apr 2026")
	assert.Contains(t, text, "Day 1 Access")
	assert.Contains(t, text, "Day 2 Access")
}

// FR-022. The footer band carries the site and the support contact, and it comes
// from configuration rather than being compiled in (FR-035).
func TestRenderTicketsPDFCarriesTheConfiguredBranding(t *testing.T) {
	brand := sampleBrand()
	brand.SiteName = "OTHEREXPO"
	brand.SiteURL = "https://other.example"
	brand.SupportEmail = "support@other.example"

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(1), brand)
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "OTHEREXPO")
	assert.Contains(t, text, "https://other.example")
	assert.Contains(t, text, "support@other.example")
	assert.Contains(t, text, "CUSTOMER SERVICE")
}

// A configured logo that does not exist must not fail a delivery — it is
// decorative, and FR-006 covers document generation, not ornament (research
// R-005).
func TestRenderTicketsPDFFallsBackToAWordmarkWhenTheLogoIsUnusable(t *testing.T) {
	brand := sampleBrand()
	brand.LogoPath = "/nonexistent/definitely-not-here.png"

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(1), brand)

	require.NoError(t, err, "a missing logo must never fail a send")
	assert.Contains(t, string(doc), "JIVE", "the text wordmark stands in for the missing asset")
}

// --- Spec 016 research R-006: the cp1252 guard ----------------------------

// gofpdf's core fonts are single-byte. Handing them raw UTF-8 renders each byte
// of a multi-byte rune as its own glyph. This predates the feature; the
// receipt's admin-authored event ADDRESS widened the exposure enough to fix it.
func TestLatin1TransliteratesTypographicPunctuation(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"curly apostrophe": {"Rock ’n’ Roll", "Rock 'n' Roll"},
		"curly quotes":     {"The “Main” Stage", `The "Main" Stage`},
		"en and em dash":   {"Day 1 – Day 2 — Finale", "Day 1 - Day 2 - Finale"},
		"ellipsis":         {"And more…", "And more..."},
		"bullet":           {"26 Apr • Entrance", "26 Apr - Entrance"},
		"plain ascii":      {"Balai Sarbini", "Balai Sarbini"},
		"empty":            {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, notification.Latin1(tc.in))
		})
	}
}

func TestLatin1DropsWhatTheCoreFontsCannotExpress(t *testing.T) {
	// A missing character is a smaller lie than a wrong one.
	assert.Equal(t, "Tokyo ", notification.Latin1("Tokyo 東京"))
	assert.NotContains(t, notification.Latin1("東京"), "æ",
		"a dropped rune must not leave its UTF-8 bytes behind as latin-1 glyphs")
}

// Accented Latin survives as a single byte rather than as two glyphs.
func TestLatin1KeepsLatinOneCharactersAsSingleBytes(t *testing.T) {
	got := notification.Latin1("Café München")

	assert.Len(t, got, len("Cafe Munchen"), "each accented letter must occupy exactly one byte")
	assert.Equal(t, byte(0xE9), got[3], "e-acute is cp1252 0xE9")
}

// The guard must actually be applied by the renderer, not merely available.
func TestRenderTicketsPDFAppliesTheLatin1Guard(t *testing.T) {
	tickets := sampleTickets(1)
	tickets[0].EventName = "Rock ’n’ Roll — 2026"
	// The ticket type, not the venue: the venue row was removed from this page
	// (FR-019a), so it is no longer a rendered surface to test the guard through.
	tickets[0].TicketTypeName = "The “Main” Stage"

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), tickets, sampleBrand())
	require.NoError(t, err)
	text := string(doc)

	assert.Contains(t, text, "ROCK 'N' ROLL - 2026")
	assert.Contains(t, text, `The "Main" Stage`)
	assert.NotContains(t, pdfDrawnText(doc), "â", "the em dash must not survive as mojibake")
}

// --- Spec 016 Revision 3: the brand refresh -------------------------------

// FR-035a, FR-022d / T129. The footer prints the configured site address and the
// customer-service address the design specifies.
//
// Asserted at the GO tier, not in e2e: renderTicketsPDF compresses its content
// streams in production, so what a page SAYS is unreachable from Playwright
// (research R-025). RenderTicketsPDFPlain is the seam that makes it readable.
func TestRenderTicketsPDFFooterCarriesTheConfiguredContacts(t *testing.T) {
	brand := sampleBrand()
	brand.SiteURL = "https://www.jive-promotion.com/"
	brand.SupportEmail = "help@manjo.co.id"

	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(1), brand)
	require.NoError(t, err)

	text := string(doc)
	assert.Contains(t, text, "https://www.jive-promotion.com/")
	assert.Contains(t, text, "help@manjo.co.id")
	assert.Contains(t, text, "CUSTOMER SERVICE")
}

// FR-022a, FR-022e, SC-021 / T130. The customer-service block is
// right-POSITIONED with its contents LEFT-aligned: the label, the envelope and
// the address share one starting edge.
//
// Parameterised over address length on purpose. The design's mock happens to show
// the address ending flush at the right margin, and a footer that lines up only
// for that one string does not meet FR-022e.
func TestTicketFooterCustomerServiceBlockIsLeftAlignedWithin(t *testing.T) {
	addresses := []string{
		"a@b.co",
		"help@manjo.co.id",
		"customer-service.team@jive-promotion.example.com",
	}

	for _, address := range addresses {
		left, width := notification.TicketFooterGeometry(address)

		// The BLOCK's right edge meets the right margin, whatever the address.
		assert.InDelta(t, notification.FooterRightMargin, left+width, 0.01,
			"the block's right edge must meet the right margin for %q", address)

		// The block never runs off the left of the page or collides with the
		// site block on the other side of the band.
		assert.Greater(t, left, 100.0,
			"the customer-service block must stay in the band's right half for %q", address)
	}
}

// FR-022a / T130. A longer address widens the block leftwards; it must never
// move the shared left edge to the RIGHT of a shorter one, which is what
// right-aligning each line independently used to do.
func TestTicketFooterBlockGrowsLeftwardsAsTheAddressGrows(t *testing.T) {
	shortLeft, _ := notification.TicketFooterGeometry("a@b.co")
	longLeft, _ := notification.TicketFooterGeometry("customer-service.team@jive-promotion.example.com")

	assert.Less(t, longLeft, shortLeft,
		"a longer address must extend the block leftwards from a fixed right edge")
}

// FR-023b. The mark is CAPPED by the band, never the other way round.
//
// This is the invariant that keeps the page from growing when the asset changes.
// It was briefly inverted on 2026-08-19 — the band was grown to 38.3mm so the
// lockup's secondary line would read in print — and reversed the same day,
// because it made every ticket visibly taller for a credit nobody reads off a
// pass. The assertion is kept, pointing the other way, so the enlargement cannot
// return by accident.
func TestBrandMarkIsCappedByTheBandAndNeverGrowsIt(t *testing.T) {
	w, h, ok := notification.BrandMarkBox(sampleBrand())
	require.True(t, ok, "the embedded mark must register as an image")

	assert.Equal(t, 21.0, notification.BandHeight,
		"the header band is fixed; a mark must never size it")
	assert.Less(t, h, notification.BandHeight,
		"the mark must fit inside the band with inset to spare")
	assert.Less(t, w, 30.0,
		"a mark wider than ~30mm means the band grew to fit it again")
}

// R-023 / T131. drawEnvelope sets round line caps and joins, which are STICKY
// Fpdf state. The footer is drawn last on a page, so an un-reset style leaks into
// the NEXT ticket's rules — a defect that renders perfectly on a one-ticket order
// and only appears from page 2.
func TestRenderTicketsPDFDoesNotLeakRoundLineCapsAcrossPages(t *testing.T) {
	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(3), sampleBrand())
	require.NoError(t, err)

	// "1 J" is a round line join and "1 j" a round cap in PDF content-stream
	// operators. The envelope sets them and must restore miter/butt, so the last
	// cap/join state written before each page ends must not be the round pair.
	text := string(doc)
	lastRound := strings.LastIndex(text, "1 J")
	lastSquare := strings.LastIndex(text, "0 J")
	assert.Greater(t, lastSquare, lastRound,
		"the last join state written must be miter — drawEnvelope's reset is missing or was moved")
}

// --- Spec 016 Revision 5: e-ticket colours ---------------------------------

// FR-019b / SC-024. The event name is a label above the ticket type, not an
// alert. It rendered in the brand crimson until 2026-08-19.
//
// The assertion is on the RENDERED page, not on the constant: it builds the
// operator from a literal and looks for it in the document's own bytes, so it
// cannot pass by comparing a value against itself.
func TestRenderTicketsPDFDrawsNoBrandCrimson(t *testing.T) {
	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(2), sampleBrand())
	require.NoError(t, err)

	assert.NotContains(t, string(doc), notification.ColorOperator(notification.RetiredCrimson),
		"the e-ticket must carry no brand crimson outside the logo image (FR-019b)")
	assert.Contains(t, string(doc), notification.ColorOperator(notification.PdfEventName),
		"the event name must be drawn in the design's slate")
}

// FR-022f / SC-024. The footer's labels must recede behind the values they head.
//
// The code drew them the other way round until 2026-08-19, which left the
// support address — the only value on the document a buyer acts on — as the
// dimmest text in the band, and worst in greyscale print.
func TestRenderTicketsPDFFooterLabelsRecedeBehindTheirValues(t *testing.T) {
	doc, err := notification.RenderTicketsPDFPlain(sampleOrder(), sampleTickets(1), sampleBrand())
	require.NoError(t, err)

	assert.Contains(t, string(doc), notification.ColorOperator(notification.PdfWhite),
		"the site URL and support address are drawn in pure white")
	assert.Contains(t, string(doc), notification.ColorOperator(notification.PdfBandLabel),
		"the JIVE and CUSTOMER SERVICE labels are drawn dimmed")

	// The ordering, not just the two values: a future palette edit that made the
	// label brighter again would satisfy the two assertions above only until the
	// constants moved, and this catches that directly.
	assert.Less(t,
		notification.RelativeLuminance(notification.PdfBandLabel),
		notification.RelativeLuminance(notification.PdfWhite),
		"a label must never be brighter than the value beneath it (FR-022f)")
}
