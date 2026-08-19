package notification

import "strings"

// Masking windows (spec 016 FR-033). One rule, applied identically to the email
// body and the receipt: the two surfaces render the same stored value, and two
// masking shapes for one value read as two different buyers.
const (
	// emailMaskTrailing is how much of an address's local part is hidden.
	// Everything before it is printed verbatim.
	//
	// This replaced a keep-the-first-5 rule (2026-08-19, spec FR-033), which put
	// the asterisks at the front and — worse — returned a local part of exactly
	// five characters UNCHANGED, so a masking function handed back its input.
	emailMaskTrailing = 3
	// phoneMaskTrailing is how much of a phone number is hidden. Everything
	// before it is printed verbatim.
	//
	// This replaced a keep-5-leading/keep-4-trailing rule that masked the MIDDLE
	// (2026-08-19, spec FR-033). It discloses more of the number than that rule
	// did, and that was chosen deliberately: a buyer has to recognise their own
	// number on their own receipt. Do not "restore" the middle mask as a privacy
	// fix — see spec.md §Assumptions and research R-027.
	phoneMaskTrailing = 4
)

// MaskEmail partially masks an address for display, replacing the last
// emailMaskTrailing characters of the local part with asterisks and keeping the
// whole domain: "dimasprasetyo@gmail.com" becomes "dimasprase***@gmail.com".
//
// The domain survives deliberately: a buyer forwarding a receipt to their
// finance team still needs the address to be recognisable, and the domain is not
// the identifying half. The receipt mock masks it too, inconsistently with the
// body mock — spec Assumptions records that as the discretionary call it is.
//
// A value with no "@" is treated as a bare local part under the same rule. That
// discloses more of such a value than the superseded rule did, and was accepted
// knowingly: the field is format-validated at checkout, so the branch is close to
// unreachable, and a second masking rule would be a second thing to keep correct.
func MaskEmail(addr string) string {
	if addr == "" {
		// Empty means unrecorded. Callers omit the row entirely rather than
		// rendering asterisks, which would claim a value exists.
		return ""
	}

	// LastIndex, not Index: an address may legally carry "@" inside a quoted
	// local part, and the domain is always what follows the final one.
	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return maskEmailLocal(addr)
	}

	local, domain := addr[:at], addr[at:]
	return maskEmailLocal(local) + domain
}

// MaskPhone partially masks a phone number, replacing its last
// phoneMaskTrailing characters with asterisks and printing everything before
// them verbatim: "+628123456789" becomes "+628123456****".
//
// It counts CHARACTERS, not digits, so a leading "+" and any spacing the buyer
// typed survive exactly as stored. Normalising to a country-code format first
// would reformat the buyer's own phone number on their own receipt, which
// FR-033 forbids in as many words.
func MaskPhone(phone string) string {
	if phone == "" {
		// Empty means unrecorded. Callers omit the row entirely rather than
		// rendering asterisks, which would claim a value exists.
		return ""
	}

	runes := []rune(phone)
	// "Everything except the last four" is nothing here, so the value is masked
	// entirely rather than partly disclosed. Note this is stricter than the rule
	// it replaced, which printed a leading character for a four-digit value.
	if len(runes) <= phoneMaskTrailing {
		return strings.Repeat("*", len(runes))
	}

	// One asterisk per replaced character, so the result never changes length —
	// the receipt aligns its Order Details column against it.
	return string(runes[:len(runes)-phoneMaskTrailing]) +
		strings.Repeat("*", phoneMaskTrailing)
}

// maskEmailLocal hides the trailing emailMaskTrailing characters of a local part,
// always leaving at least one character visible.
//
// Kept separate from MaskPhone rather than shared. The two are now the same
// shape, which is tempting, but their FLOORS come from different requirements: a
// phone of four characters or fewer is masked entirely, while a local part always
// keeps one character — a two-character local renders exactly one asterisk, which
// FR-033 asks for directly. A shared helper would carry that difference as a
// boolean literal no reader can decode at the call site.
func maskEmailLocal(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}

	n := emailMaskTrailing
	if most := len(runes) - 1; n > most {
		n = most
	}
	if n < 1 {
		// A single character cannot both keep one and hide one. Returning it
		// whole would disclose the value entirely, so it is masked.
		return "*"
	}

	// One asterisk per replaced character, so the result never grows — the
	// receipt aligns its Order Details column against it.
	return string(runes[:len(runes)-n]) + strings.Repeat("*", n)
}
