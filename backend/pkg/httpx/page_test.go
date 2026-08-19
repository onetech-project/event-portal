package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// --- PageRequest clamping ---------------------------------------------------
//
// Clamping is total: a PageRequest cannot hold an invalid value, which is what
// lets every caller downstream skip re-checking (spec 021 FR-013, FR-014).

func TestNewPageRequestDefaultsWhenNothingWasAsked(t *testing.T) {
	p := httpx.NewPageRequest(0, 0)

	assert.Equal(t, 1, p.Page)
	assert.Equal(t, httpx.DefaultPageSize, p.Size)
}

func TestNewPageRequestRaisesAnImpossiblePageToTheFirst(t *testing.T) {
	for _, requested := range []int{0, -1, -3, -9999} {
		p := httpx.NewPageRequest(requested, 20)
		assert.Equal(t, 1, p.Page, "page %d", requested)
	}
}

func TestNewPageRequestFallsBackToTheDefaultSizeWhenSizeIsNotPositive(t *testing.T) {
	for _, requested := range []int{0, -1, -50} {
		p := httpx.NewPageRequest(1, requested)
		assert.Equal(t, httpx.DefaultPageSize, p.Size, "size %d", requested)
	}
}

func TestNewPageRequestCapsAnOversizedPage(t *testing.T) {
	// FR-014: a request must not be able to demand an unbounded page. 5000 is
	// reduced to the maximum rather than refused.
	p := httpx.NewPageRequest(1, 5000)

	assert.Equal(t, httpx.MaxPageSize, p.Size)
}

func TestNewPageRequestKeepsAValidRequestExactly(t *testing.T) {
	p := httpx.NewPageRequest(4, 50)

	assert.Equal(t, 4, p.Page)
	assert.Equal(t, 50, p.Size)
	assert.Equal(t, 150, p.Offset())
	assert.Equal(t, 50, p.Limit())
}

func TestOffsetOfTheFirstPageIsZero(t *testing.T) {
	assert.Equal(t, 0, httpx.NewPageRequest(1, 20).Offset())
}

// --- ClampTo ----------------------------------------------------------------

func TestClampToPullsAPageBeyondTheEndBackToTheLastOne(t *testing.T) {
	// FR-013: a stale bookmark resolves to the last page, never an error and
	// never an empty table.
	p := httpx.NewPageRequest(999, 20).ClampTo(137)

	assert.Equal(t, 7, p.Page, "137 rows at 20 per page is 7 pages")
	assert.Equal(t, 120, p.Offset())
}

func TestClampToLeavesAnInRangePageAlone(t *testing.T) {
	p := httpx.NewPageRequest(3, 20).ClampTo(137)

	assert.Equal(t, 3, p.Page)
}

func TestClampToOnAnEmptyResultIsTheFirstPage(t *testing.T) {
	p := httpx.NewPageRequest(9, 20).ClampTo(0)

	assert.Equal(t, 1, p.Page)
	assert.Equal(t, 0, p.Offset())
}

// --- Page invariants --------------------------------------------------------

func TestNewPageReportsTheServedPositionNotTheRequestedOne(t *testing.T) {
	req := httpx.NewPageRequest(999, 20).ClampTo(137)
	page := httpx.NewPage([]string{"a", "b"}, req, 137)

	assert.Equal(t, 7, page.Page)
	assert.Equal(t, 20, page.PageSize)
	assert.Equal(t, int64(137), page.Total)
	assert.Equal(t, 7, page.TotalPages)
}

func TestNewPageRoundsPartialPagesUp(t *testing.T) {
	cases := []struct {
		total int64
		size  int
		want  int
	}{
		{total: 0, size: 20, want: 0},
		{total: 1, size: 20, want: 1},
		{total: 20, size: 20, want: 1},
		{total: 21, size: 20, want: 2},
		{total: 137, size: 20, want: 7},
		{total: 100, size: 100, want: 1},
	}
	for _, c := range cases {
		page := httpx.NewPage([]int{}, httpx.NewPageRequest(1, c.size), c.total)
		assert.Equal(t, c.want, page.TotalPages, "total %d size %d", c.total, c.size)
	}
}

func TestAnEmptyPageMarshalsItemsAsAnArrayNotNull(t *testing.T) {
	// A `null` here would make every consumer defend against it. The frontend
	// maps over items unconditionally.
	page := httpx.NewPage[string](nil, httpx.NewPageRequest(1, 20), 0)

	raw, err := json.Marshal(page)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"items":[]`)
	assert.NotContains(t, string(raw), `"items":null`)
}

func TestPageCarriesTheContractedFieldNames(t *testing.T) {
	raw, err := json.Marshal(httpx.NewPage([]string{"x"}, httpx.NewPageRequest(2, 20), 40))
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	for _, field := range []string{"items", "page", "page_size", "total", "total_pages"} {
		assert.Contains(t, decoded, field)
	}
}

func TestPageNeverHoldsMoreItemsThanTheSizeAllows(t *testing.T) {
	// SC-002 is a property of the query, but the type should not be able to
	// misreport it: whatever rows are handed in, PageSize is what was asked for.
	page := httpx.NewPage([]int{1, 2, 3}, httpx.NewPageRequest(1, 20), 3)

	assert.LessOrEqual(t, len(page.Items), page.PageSize)
}

// --- BindPage ---------------------------------------------------------------
//
// SC-007: no value a person could type into an address bar produces an error.

func TestBindPageReadsAValidQuery(t *testing.T) {
	p := bindQuery(t, "?page=3&page_size=50")

	assert.Equal(t, 3, p.Page)
	assert.Equal(t, 50, p.Size)
}

func TestBindPageDefaultsWhenTheParamsAreAbsent(t *testing.T) {
	p := bindQuery(t, "")

	assert.Equal(t, 1, p.Page)
	assert.Equal(t, httpx.DefaultPageSize, p.Size)
}

func TestBindPageCorrectsEveryShapeOfGarbage(t *testing.T) {
	cases := []struct {
		query    string
		wantPage int
		wantSize int
	}{
		{query: "?page=0", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page=-3", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page=abc", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page=", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page=1.5", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page_size=0", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page_size=abc", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page_size=5000", wantPage: 1, wantSize: httpx.MaxPageSize},
		{query: "?page=-1&page_size=-1", wantPage: 1, wantSize: httpx.DefaultPageSize},
		{query: "?page=99999999999999999999", wantPage: 1, wantSize: httpx.DefaultPageSize},
	}
	for _, c := range cases {
		p := bindQuery(t, c.query)
		assert.Equal(t, c.wantPage, p.Page, "query %q", c.query)
		assert.Equal(t, c.wantSize, p.Size, "query %q", c.query)
	}
}

func TestBindPageTrimsSurroundingSpace(t *testing.T) {
	p := bindQuery(t, "?page=%202%20&page_size=%2050%20")

	assert.Equal(t, 2, p.Page)
	assert.Equal(t, 50, p.Size)
}

func bindQuery(t *testing.T, query string) httpx.PageRequest {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/"+query, nil)
	return httpx.BindPage(e.NewContext(req, httptest.NewRecorder()))
}

// --- Normalize --------------------------------------------------------------

func TestTheZeroValueNormalisesToTheFirstPage(t *testing.T) {
	// A caller writing OrderFilter{} means "the first page", not "no rows". The
	// zero value asking for LIMIT 0 would return an empty page for a list that
	// has rows, and empty is a legitimate answer to a different question — so the
	// mistake would not announce itself.
	var zero httpx.PageRequest

	p := zero.Normalize()

	assert.Equal(t, 1, p.Page)
	assert.Equal(t, httpx.DefaultPageSize, p.Size)
	assert.Equal(t, httpx.DefaultPageSize, p.Limit())
	assert.Equal(t, 0, p.Offset())
}

func TestNormalizeLeavesAValidRequestAlone(t *testing.T) {
	p := httpx.NewPageRequest(3, 50).Normalize()

	assert.Equal(t, 3, p.Page)
	assert.Equal(t, 50, p.Size)
}
