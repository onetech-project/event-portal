package randcode_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/randcode"
)

func TestNewReturnsTheRequestedLength(t *testing.T) {
	for _, length := range []int{1, 6, 10, 32} {
		code, err := randcode.New(length)

		require.NoError(t, err)
		assert.Len(t, code, length)
	}
}

func TestNewUsesOnlyTheUnambiguousAlphabet(t *testing.T) {
	for range 200 {
		code, err := randcode.New(12)
		require.NoError(t, err)

		for _, r := range code {
			assert.True(t, strings.ContainsRune(randcode.Alphabet, r),
				"character %q is outside the unambiguous alphabet", r)
		}
	}
}

// Codes are read aloud and typed by hand at a venue door, so the characters that
// are routinely confused for one another must not appear at all.
func TestAlphabetExcludesConfusableCharacters(t *testing.T) {
	for _, excluded := range []string{"I", "O", "0", "1"} {
		assert.NotContains(t, randcode.Alphabet, excluded)
	}
}

func TestAlphabetIsUppercaseAndUnique(t *testing.T) {
	assert.Equal(t, strings.ToUpper(randcode.Alphabet), randcode.Alphabet)

	seen := map[rune]bool{}
	for _, r := range randcode.Alphabet {
		assert.False(t, seen[r], "duplicate character %q skews the distribution", r)
		seen[r] = true
	}
}

func TestNewRejectsNonPositiveLength(t *testing.T) {
	_, err := randcode.New(0)
	require.Error(t, err)

	_, err = randcode.New(-3)
	require.Error(t, err)
}

func TestNewProducesDistinctValues(t *testing.T) {
	seen := map[string]struct{}{}
	for range 1000 {
		code, err := randcode.New(10)
		require.NoError(t, err)
		seen[code] = struct{}{}
	}
	assert.Len(t, seen, 1000)
}
