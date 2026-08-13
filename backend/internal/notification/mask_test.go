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
		"ordinary address keeps five and the whole domain": {
			in: "dimasprasetyo@gmail.com", want: "dimas********@gmail.com",
		},
		"local part exactly the kept prefix is unchanged": {
			in: "dimas@gmail.com", want: "dimas@gmail.com",
		},
		"local part shorter than the prefix masks from the second character": {
			in: "bud@gmail.com", want: "b**@gmail.com",
		},
		"single character local part is masked, not disclosed": {
			in: "a@gmail.com", want: "*@gmail.com",
		},
		"no at sign is treated as a local part and never printed whole": {
			in: "notanemail", want: "notan*****",
		},
		"empty stays empty so the row is omitted rather than showing asterisks": {
			in: "", want: "",
		},
		"only the last at sign splits, so a quoted local part survives": {
			in: "we.ird@thing@example.com", want: "we.ir*******@example.com",
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
		"international form keeps five leading and four trailing": {
			in: "+628123456789", want: "+6281****6789",
		},
		"local form long enough for both windows": {
			in: "081234567890", want: "08123***7890",
		},
		"exactly nine has no room for a trailing window": {
			in: "081234567", want: "08123****",
		},
		"exactly the two windows with nothing between is unchanged": {
			in: "081234567", want: "08123****",
		},
		"shorter than the leading window masks from the second character": {
			in: "0812", want: "0***",
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
