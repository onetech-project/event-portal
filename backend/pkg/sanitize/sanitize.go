// Package sanitize cleans admin-authored WYSIWYG HTML before it is stored.
// The API is the only writer of rich content, so sanitizing once on write
// keeps every read path (guest pages, dialogs, emails) dependency-free.
// Shared, non-domain utility — lives under pkg/ (Constitution Principle I).
package sanitize

import "github.com/microcosm-cc/bluemonday"

// policy is bluemonday's user-generated-content policy, extended with the
// class attribute on block/inline text elements so editor alignment classes
// survive. Script/style/event handlers are stripped outright.
var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").OnElements("p", "h1", "h2", "h3", "h4", "li", "ol", "ul", "span", "blockquote")
	return p
}()

// HTML returns the sanitized form of admin-authored rich text. Safe to call on
// empty strings.
func HTML(in string) string {
	return policy.Sanitize(in)
}
