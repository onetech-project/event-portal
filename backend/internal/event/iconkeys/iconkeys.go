// Package iconkeys holds the recognized icon-key catalog for CMS content
// blocks (spec 009). icon_keys.txt is GENERATED from the frontend's installed
// lucide-react by frontend/scripts/generate-icon-keys.mjs — never hand-edited —
// so the set the admin picker offers and the set validateIcon accepts stay the
// same file. A frontend vitest test fails whenever the checked-in file drifts
// from the installed package.
package iconkeys

import (
	_ "embed"
	"strings"
)

//go:embed icon_keys.txt
var raw string

var keys = func() map[string]struct{} {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	set := make(map[string]struct{}, len(lines))
	for _, l := range lines {
		if l != "" {
			set[l] = struct{}{}
		}
	}
	return set
}()

// Valid reports whether key is part of the recognized icon catalog.
func Valid(key string) bool {
	_, ok := keys[key]
	return ok
}

// Count returns the catalog size; used by tests to assert the embedded file
// is the real generated catalog rather than a stub.
func Count() int {
	return len(keys)
}
