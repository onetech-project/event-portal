package notification

import (
	_ "embed"
	"os"
	"strings"
)

// Brand assets are compiled into the binary rather than read from disk at
// runtime (spec 016 T066).
//
// The runtime image is `gcr.io/distroless/static-debian12:nonroot` — there is no
// shell, no package manager, and no guarantee about the process's working
// directory. A relative asset path would work in every local test and fail in
// the container, which is the worst shape a deployment bug can take. Embedding
// makes the asset unconditionally present.
//
// The configured path remains as an override, for an operator who needs to
// rebrand without a rebuild.
//
//go:embed assets/brand/jive-logo-white.png
var embeddedLogo []byte

// The location pin drawn before the venue in the email body (FR-025).
//
// AUTHORED, not extracted (spec 016 T069). The design's pin is the `📍` emoji as
// a text layer, which Figma cannot export — every route returns a blank image —
// and FR-025 forbids shipping the character itself, because each platform draws
// its own. So this is a plain map-pin silhouette drawn to match the design's
// weight and size. It is the one glyph in this feature that is ours rather than
// the designer's, and it is worth a look before release.
//
//go:embed assets/icons/location-pin.png
var embeddedPin []byte

// pinPNG returns the location-pin icon, preferring an operator-supplied file.
func pinPNG(brand Branding) []byte {
	if path := strings.TrimSpace(brand.PinPath); path != "" {
		if content, err := os.ReadFile(path); err == nil && len(content) > 0 {
			return content
		}
	}
	return embeddedPin
}

// logoPNG returns the brand mark to draw, preferring an operator-supplied file
// over the embedded default.
//
// A configured path that cannot be read falls back to the embedded asset rather
// than to nothing: a typo in an environment variable must not silently strip the
// logo from every document (FR-023a).
func logoPNG(brand Branding) []byte {
	if path := strings.TrimSpace(brand.LogoPath); path != "" {
		if content, err := os.ReadFile(path); err == nil && len(content) > 0 {
			return content
		}
	}
	return embeddedLogo
}
