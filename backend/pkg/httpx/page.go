package httpx

import (
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// Paging defaults and the enforced ceiling (spec 021 FR-012, FR-014). The
// maximum is not advisory: a caller asking for five thousand rows gets a hundred,
// because "no page size at all" is the failure mode pagination exists to remove.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// PageRequest is what a caller asked for, after correction. Construct it only
// through NewPageRequest or BindPage, so it can never hold a value the rest of
// the system would have to defend against.
//
// Correction rather than rejection is deliberate, and only applies to paging: a
// bad `status` means the caller named something that does not exist and is told
// so, while a bad page number means the caller's position drifted — after a
// deletion, or from a stale bookmark — and the useful answer is the nearest real
// page (spec 021 FR-013, SC-007).
type PageRequest struct {
	Page int // 1-based, always >= 1
	Size int // always within [1, MaxPageSize]
}

// NewPageRequest corrects a requested page and size into a usable pair.
func NewPageRequest(page, size int) PageRequest {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	return PageRequest{Page: page, Size: size}
}

// BindPage reads `page` and `page_size` off the query string. It cannot fail:
// anything unparseable is treated as absent and takes the default.
func BindPage(c echo.Context) PageRequest {
	return NewPageRequest(queryInt(c, "page"), queryInt(c, "page_size"))
}

// queryInt returns 0 for absent, blank, or unparseable values — including ones
// that overflow an int — leaving NewPageRequest to apply the default. Callers
// never see the difference between "not asked" and "asked incoherently", which
// is the point.
func queryInt(c echo.Context, name string) int {
	raw := strings.TrimSpace(c.QueryParam(name))
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

// Normalize makes a PageRequest safe to use however it was built.
//
// The zero value is the reason this exists: a caller writing OrderFilter{} would
// otherwise ask for LIMIT 0 and get an empty page for a list that has rows —
// silently, since zero rows is a legitimate answer to some other question. A
// service normalizes what it is handed before it touches the value, so the only
// way to reach a query is through a corrected pair.
func (p PageRequest) Normalize() PageRequest { return NewPageRequest(p.Page, p.Size) }

// Offset is the row offset this page starts at.
func (p PageRequest) Offset() int { return (p.Page - 1) * p.Size }

// Limit is how many rows to read.
func (p PageRequest) Limit() int { return p.Size }

// ClampTo pulls a page that is past the end back to the last page that exists.
//
// This is why the total has to be counted before the slice is taken: a page read
// at an offset beyond the end comes back empty, and an empty result cannot tell
// you which page you should have asked for.
func (p PageRequest) ClampTo(total int64) PageRequest {
	last := totalPages(total, p.Size)
	if last == 0 {
		return PageRequest{Page: 1, Size: p.Size}
	}
	if p.Page > last {
		return PageRequest{Page: last, Size: p.Size}
	}
	return p
}

// Page is one page of a list, in the shape every paginated endpoint returns
// inside the standard {code, message, data} envelope.
//
// T is always the owning domain's own DTO — this type wraps a domain's wire
// shape, it does not define one (Constitution Principle III).
type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// NewPage assembles a page. req MUST already have been clamped against total, so
// that Page reports the position actually served rather than the one requested.
//
// A nil slice becomes an empty one: `"items": null` would make every consumer
// defend against a case the API should simply never produce.
func NewPage[T any](items []T, req PageRequest, total int64) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{
		Items:      items,
		Page:       req.Page,
		PageSize:   req.Size,
		Total:      total,
		TotalPages: totalPages(total, req.Size),
	}
}

// EmptyPage is the answer when a filter cannot match anything at all — an event
// with no ticket types, say — and no query is worth running.
func EmptyPage[T any](req PageRequest) Page[T] {
	return NewPage([]T{}, req.ClampTo(0), 0)
}

// totalPages rounds up, and is 0 rather than 1 for an empty result: "page 1 of 0"
// is not a thing an operator should ever be shown.
func totalPages(total int64, size int) int {
	if total <= 0 || size <= 0 {
		return 0
	}
	return int((total + int64(size) - 1) / int64(size))
}
