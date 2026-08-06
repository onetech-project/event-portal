package sanitize_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/manjo/ticketing/backend/pkg/sanitize"
)

func TestHTMLStripsScripts(t *testing.T) {
	out := sanitize.HTML(`<p>hello</p><script>alert(1)</script>`)

	assert.Equal(t, "<p>hello</p>", out)
}

func TestHTMLStripsEventHandlers(t *testing.T) {
	out := sanitize.HTML(`<p onclick="steal()">hi</p><img src=x onerror=alert(1)>`)

	assert.NotContains(t, out, "onclick")
	assert.NotContains(t, out, "onerror")
	assert.Contains(t, out, "hi")
}

func TestHTMLKeepsFormattingAndAlignmentClasses(t *testing.T) {
	in := `<h2 class="text-center">Terms</h2><ol><li class="font-bold">One</li></ol><p><strong>bold</strong> and <em>italic</em></p>`

	out := sanitize.HTML(in)

	assert.Contains(t, out, `<h2 class="text-center">`)
	assert.Contains(t, out, `<li class="font-bold">`)
	assert.Contains(t, out, "<strong>bold</strong>")
	assert.Contains(t, out, "<em>italic</em>")
}

func TestHTMLKeepsSafeLinksAndDropsJavascriptHrefs(t *testing.T) {
	out := sanitize.HTML(`<a href="https://example.com">ok</a><a href="javascript:alert(1)">bad</a>`)

	assert.Contains(t, out, `https://example.com`)
	assert.NotContains(t, out, "javascript:")
}

func TestHTMLEmptyInput(t *testing.T) {
	assert.Empty(t, sanitize.HTML(""))
}
