// Package randcode generates the human-readable random codes used for order
// numbers and ticket codes.
package randcode

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
)

// Alphabet deliberately omits I, O, 0, and 1. Ticket codes are read aloud and
// typed by hand at a venue door, where those four are the characters people
// confuse; excluding them costs a little entropy per character and removes an
// entire class of failed check-ins.
const Alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// New returns a cryptographically random code of the given length. Ticket codes
// are the bearer credential for entry, so a predictable generator would let anyone
// mint valid-looking codes.
func New(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("randcode: length must be positive")
	}

	var sb strings.Builder
	sb.Grow(length)

	max := big.NewInt(int64(len(Alphabet)))
	for range length {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", errors.New("randcode: no entropy available")
		}
		sb.WriteByte(Alphabet[n.Int64()])
	}
	return sb.String(), nil
}
