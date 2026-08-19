package notification_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/manjo/ticketing/backend/internal/notification"
)

// Spec 016 FR-033. The rule is one rule on both surfaces: the email body's Buyer
// Information block and the receipt's Order Details block render the same value
// masked the same way, because two shapes for one value read as two buyers.
//
// The cases below are the full table from research R-003, degenerate inputs
// included — those are the ones that turn a masking helper into a disclosure.
func TestMaskEmail(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"ordinary address masks only the trailing three of the local part": {
			in: "dimasprasetyo@gmail.com", want: "dimasprase***@gmail.com",
		},
		"five-character local part is no longer printed whole": {
			// Under the superseded keep-first-5 rule this returned UNCHANGED — a
			// masking function handing back its input. FR-033's floor now hides at
			// least one character always.
			in: "dimas@gmail.com", want: "di***@gmail.com",
		},
		"four characters coincide with the superseded rule": {
			in: "budi@gmail.com", want: "b***@gmail.com",
		},
		"three characters keep one visible and mask the rest": {
			in: "bud@gmail.com", want: "b**@gmail.com",
		},
		"two characters get exactly one asterisk": {
			in: "ab@gmail.com", want: "a*@gmail.com",
		},
		"single character cannot keep one and hide one, so it is masked entirely": {
			in: "a@gmail.com", want: "*@gmail.com",
		},
		"no at sign is treated as a local part under the same rule": {
			// Accepted knowingly in clarification: this discloses more than the old
			// rule did. The field is format-validated at checkout, so the branch is
			// close to unreachable, and a second masking rule would be a second
			// thing to keep correct.
			in: "notanemail", want: "notanem***",
		},
		"empty stays empty so the row is omitted rather than showing asterisks": {
			in: "", want: "",
		},
		"only the last at sign splits, so a quoted local part survives": {
			in: "we.ird@thing@example.com", want: "we.ird@th***@example.com",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, notification.MaskEmail(tc.in))
		})
	}
}

// The phone rule counts CHARACTERS, not digits, so a leading + and any spacing
// survive verbatim. Normalising to digits first would reformat the buyer's own
// phone number on their own receipt, which is a surprise nobody asked for.
func TestMaskPhone(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"international form masks only the trailing four": {
			in: "+6281234567890", want: "+628123456****",
		},
		"one character shorter, one character less kept": {
			in: "+628123456789", want: "+62812345****",
		},
		"local form masks only the trailing four": {
			in: "081234567890", want: "08123456****",
		},
		"nine characters, same shape as the superseded rule by coincidence": {
			in: "081234567", want: "08123****",
		},
		"separators the buyer typed survive verbatim": {
			in: "+62 812 3456 7890", want: "+62 812 3456 ****",
		},
		"exactly the trailing window is masked entirely": {
			in: "0812", want: "****",
		},
		"single character is masked": {
			in: "0", want: "*",
		},
		"empty stays empty so the row is omitted": {
			in: "", want: "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, notification.MaskPhone(tc.in))
		})
	}
}

// Masking must never lengthen a value: the receipt aligns its Order Details
// column, and a helper that padded to a fixed width would shift it.
func TestMaskingNeverLengthensAValue(t *testing.T) {
	for _, in := range []string{"dimasprasetyo@gmail.com", "a@b.co", "notanemail", ""} {
		assert.LessOrEqual(t, len(notification.MaskEmail(in)), len(in), in)
	}
	for _, in := range []string{"+628123456789", "0812", "0", ""} {
		assert.LessOrEqual(t, len(notification.MaskPhone(in)), len(in), in)
	}
}
