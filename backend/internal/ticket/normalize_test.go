package ticket_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/randcode"
)

func TestNormalizeUppercasesAndTrims(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already canonical", "ABC234", "ABC234"},
		{"lowercase", "abc234", "ABC234"},
		{"mixed case", "aBc234", "ABC234"},
		{"surrounding spaces", "   ABC234   ", "ABC234"},
		{"trailing newline from a scanner", "ABC234\n", "ABC234"},
		{"tab padded", "\tABC234\t", "ABC234"},
		{"lowercase and padded", "  abc234 ", "ABC234"},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ticket.NormalizeCode(tc.input))
		})
	}
}

// Normalization touches the INPUT only. Stored codes are already canonical, so the
// query binds this value to a plain equality predicate and the btree index on
// tickets.ticket_code stays usable — wrapping the column in UPPER() would force a
// sequential scan, and SCHEMA.md is locked so no functional index can be added.
func TestNormalizeIsIdempotent(t *testing.T) {
	once := ticket.NormalizeCode("  abc234 ")

	assert.Equal(t, once, ticket.NormalizeCode(once))
}

func TestNormalizeDoesNotStripInteriorCharacters(t *testing.T) {
	// Only surrounding whitespace is removed; anything else stays so a genuinely
	// malformed code is reported as invalid rather than silently "repaired".
	assert.Equal(t, "ABC 234", ticket.NormalizeCode(" abc 234 "))
	assert.Equal(t, "ABC-234", ticket.NormalizeCode("abc-234"))
}

func TestGenerateCodeMatchesTheCanonicalForm(t *testing.T) {
	for range 200 {
		code, err := ticket.GenerateCode()

		require.NoError(t, err)
		assert.Len(t, code, ticket.CodeLength)
		assert.Equal(t, code, ticket.NormalizeCode(code), "generated codes are already canonical")
		for _, r := range code {
			assert.Contains(t, randcode.Alphabet, string(r))
		}
	}
}

func TestGeneratedCodeFitsTheColumn(t *testing.T) {
	code, err := ticket.GenerateCode()

	require.NoError(t, err)
	assert.LessOrEqual(t, len(code), 100, "tickets.ticket_code is VARCHAR(100)")
}

func TestGeneratedCodesAreDistinct(t *testing.T) {
	seen := map[string]struct{}{}
	for range 2000 {
		code, err := ticket.GenerateCode()
		require.NoError(t, err)
		seen[code] = struct{}{}
	}
	assert.Len(t, seen, 2000)
}
