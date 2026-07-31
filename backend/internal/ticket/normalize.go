// Package ticket owns the tickets table: code generation after payment, the
// public lookup, and the admin validate / mark-used flow at the door.
package ticket

import (
	"fmt"
	"strings"

	"github.com/manjo/ticketing/backend/pkg/randcode"
)

// CodeLength is the number of characters in a ticket code. Ten characters from the
// 32-symbol unambiguous alphabet is ~1.1e15 combinations — far beyond guessing,
// while still short enough to read aloud at a door.
const CodeLength = 10

// NormalizeCode canonicalizes an INPUT code: surrounding whitespace is trimmed
// (scanners and copy-paste routinely add it) and the result is uppercased.
//
// Only the input is normalized. Stored codes are written in canonical form, so
// callers bind this value to a plain `ticket_code = $1` predicate and
// idx_tickets_ticket_code is used. Wrapping the column in UPPER() instead would
// make that btree index unusable, and SCHEMA.md is LOCKED so no functional index
// can be added to compensate.
//
// Interior characters are left alone: a code with a stray character is genuinely
// invalid and must be reported as such, not silently repaired into someone else's
// ticket.
func NormalizeCode(input string) string {
	return strings.ToUpper(strings.TrimSpace(input))
}

// GenerateCode draws a new ticket code in canonical form. Codes are the bearer
// credential for entry, so the source is cryptographic — a predictable generator
// would let anyone mint a valid-looking ticket.
func GenerateCode() (string, error) {
	code, err := randcode.New(CodeLength)
	if err != nil {
		return "", fmt.Errorf("generate ticket code: %w", err)
	}
	return code, nil
}
