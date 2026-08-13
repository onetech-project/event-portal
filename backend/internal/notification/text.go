package notification

import "strings"

// latin1 makes a string safe to draw with gofpdf's core fonts.
//
// Those fonts are single-byte (cp1252). Handing them raw UTF-8 does not fail —
// it renders each byte of a multi-byte rune as its own glyph, so a typographic
// apostrophe becomes "â€™" on a printed ticket. That defect predates this
// feature: pdf.go has always written the event name, venue and attendee name
// straight into MultiCell. Spec 016 widens the exposure materially by putting the
// admin-authored event ADDRESS and the line descriptors onto a printed page, so
// the guard lands here (research R-006).
//
// The transliterations cover what actually reaches these fields — text pasted
// from a word processor. Anything else above U+00FF is dropped rather than
// mangled: a missing character is a smaller lie than a wrong one, and a venue
// name in a script these fonts cannot express is not rescuable by substitution.
//
// The proper fix is an embedded UTF-8 TrueType font via AddUTF8Font, which is the
// only way to render, say, Japanese. That needs a font file and a licence review,
// both orthogonal to attaching a receipt — recorded in R-006 so the next person
// finds the reasoning rather than the symptom.
//
// The returned string is deliberately NOT valid UTF-8 above U+007F: it is a
// cp1252 byte string, which is exactly what gofpdf wants. Never route it back
// through anything that assumes UTF-8.
func latin1(s string) string {
	if s == "" {
		return ""
	}
	// Fast path: pure ASCII, which nearly every value is.
	if isASCII(s) {
		return s
	}

	var out strings.Builder
	out.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out.WriteByte(byte(r))
		case r == '‘' || r == '’' || r == '‚':
			out.WriteByte('\'')
		case r == '“' || r == '”' || r == '„':
			out.WriteByte('"')
		case r == '–' || r == '—' || r == '−':
			out.WriteByte('-')
		case r == '…':
			out.WriteString("...")
		case r == ' ' || r == ' ' || r == ' ':
			// Non-breaking spaces of various widths. Drawing them as a plain
			// space keeps the line breaking sane.
			out.WriteByte(' ')
		case r == '•':
			// Bullet — the separator the receipt's product sub-line uses.
			out.WriteByte('-')
		case r <= 0xFF:
			// Already a single-byte codepoint: write the byte, not its UTF-8
			// encoding. This is what keeps accented Latin names correct.
			out.WriteByte(byte(r))
		default:
			// Dropped. See the doc comment.
		}
	}
	return out.String()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
