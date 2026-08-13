package notification

import "strings"

// Masking windows (spec 016 FR-033). One rule, applied identically to the email
// body and the receipt: the two surfaces render the same stored value, and two
// masking shapes for one value read as two different buyers.
const (
	// emailKeep is how much of an address's local part stays legible. Five is
	// enough for the owner to recognise their own address on a receipt they are
	// forwarding, and short enough that the rest is not a disclosure.
	emailKeep = 5
	// phoneKeepLeading covers a country or network prefix; phoneKeepTrailing
	// covers the digits people actually use to identify their own number.
	phoneKeepLeading  = 5
	phoneKeepTrailing = 4
)

// MaskEmail partially masks an address for display, keeping the first
// emailKeep characters of the local part and the whole domain.
//
// The domain survives deliberately: a buyer forwarding a receipt to their
// finance team still needs the address to be recognisable, and the domain is not
// the identifying half. The receipt mock masks it too, inconsistently with the
// body mock — spec Assumptions records that as the discretionary call it is.
//
// A value with no "@" is treated as a bare local part rather than passed
// through: whatever it is, it was stored in a contact field, so printing it
// whole is the one outcome this function exists to prevent.
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
		return maskFrom(addr, emailKeep)
	}

	local, domain := addr[:at], addr[at:]
	return maskFrom(local, emailKeep) + domain
}

// MaskPhone partially masks a phone number, keeping phoneKeepLeading leading and
// phoneKeepTrailing trailing characters.
//
// It counts CHARACTERS, not digits, so a leading "+" and any spacing the buyer
// typed survive verbatim. Normalising to digits first would reformat the buyer's
// own phone number on their own receipt.
func MaskPhone(phone string) string {
	if phone == "" {
		return ""
	}

	runes := []rune(phone)
	// Too short to carry both windows: fall back to the same
	// mask-from-the-second-character rule an unmaskable email gets, so a short
	// value is never printed whole.
	if len(runes) <= phoneKeepLeading+phoneKeepTrailing {
		return maskFrom(phone, phoneKeepLeading)
	}

	masked := make([]rune, 0, len(runes))
	masked = append(masked, runes[:phoneKeepLeading]...)
	for range len(runes) - phoneKeepLeading - phoneKeepTrailing {
		masked = append(masked, '*')
	}
	masked = append(masked, runes[len(runes)-phoneKeepTrailing:]...)
	return string(masked)
}

// maskFrom keeps the first keep characters of s and replaces the rest with
// asterisks, one per replaced character so the result never grows — the receipt
// aligns a column against it.
//
// A value at or under the keep length has nothing left to hide, so it is masked
// from its second character instead of being returned intact. That is FR-033's
// short-value fallback, and it is why a one-character value comes back as a
// single asterisk rather than as itself.
func maskFrom(s string, keep int) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	if len(runes) > keep {
		return string(runes[:keep]) + strings.Repeat("*", len(runes)-keep)
	}
	if len(runes) == keep {
		// Exactly the window: nothing was hidden and nothing needs to be.
		return s
	}
	if len(runes) == 1 {
		// "Mask from the second character" has nothing to mask here, and
		// returning the rune intact would disclose the whole value — the one
		// outcome this function exists to prevent. Mask it entirely.
		return "*"
	}
	return string(runes[:1]) + strings.Repeat("*", len(runes)-1)
}
