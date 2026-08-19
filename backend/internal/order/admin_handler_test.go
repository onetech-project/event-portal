package order_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// The HTTP edge of the paginated admin reads (spec 021).
//
// The asymmetry these tests pin is deliberate: a malformed FILTER is refused,
// because the caller named something that does not exist, while a malformed PAGE
// is corrected, because the caller named a position that drifted — after a
// deletion, or from a stale bookmark (research R8).

func newAdminOrderAPI(t *testing.T) (*echo.Echo, adminOrderFixture) {
	t.Helper()
	f := newAdminOrderFixture(t)

	events := event.NewService(f.pool, event.NewRepository(f.pool), order.NewRepository(f.pool),
		testsupport.DiscardLogger())
	svc := order.NewAdminService(order.NewRepository(f.pool), eventLookupAdapter{svc: events})

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	// No JWT middleware here: authentication has its own coverage in the admin
	// package, and mounting it would only add noise to what these assert.
	order.NewAdminHandler(svc).RegisterAdminRoutes(e.Group("/api/v1"))
	return e, f
}

type pageBody struct {
	Items      []map[string]any `json:"items"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"total_pages"`
}

func getPage(t *testing.T, e *echo.Echo, path string) (*httptest.ResponseRecorder, pageBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	var body pageBody
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	}
	return rec, body
}

// --- The paged envelope -----------------------------------------------------

func TestAdminOrdersEndpointReturnsAPageNotAnArray(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	seedOrders(t, f, 3, "ORD-HTTP")

	rec, body := getPage(t, e, "/api/v1/admin/orders?page=1&page_size=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body.Items, 2)
	assert.EqualValues(t, 3, body.Total)
	assert.Equal(t, 2, body.TotalPages)
	assert.Equal(t, 1, body.Page)
	assert.Equal(t, 2, body.PageSize)
}

func TestAdminAttendeesEndpointReturnsAPage(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ATT-HTTP", "PAID")
	for i := 0; i < 3; i++ {
		testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID,
			fmt.Sprintf("Guest %d", i), fmt.Sprintf("g%d@example.com", i))
	}

	rec, body := getPage(t, e, "/api/v1/admin/attendees?page_size=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body.Items, 2)
	assert.EqualValues(t, 3, body.Total)
}

func TestAdminFeesEndpointReturnsAPage(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	for i := 0; i < 3; i++ {
		testsupport.SeedFee(t, f.pool, fmt.Sprintf("Fee %d", i), "FIXED", "1000.00", i)
	}

	rec, body := getPage(t, e, "/api/v1/admin/fees?page_size=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, body.Items, 2)
	assert.EqualValues(t, 3, body.Total)
}

func TestAnEmptyAdminListSerializesItemsAsAnArray(t *testing.T) {
	e, _ := newAdminOrderAPI(t)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"items":[]`)
	assert.NotContains(t, rec.Body.String(), `"items":null`)
}

// --- Paging input is corrected, never refused (SC-007) ----------------------

func TestEveryShapeOfPagingGarbageStillAnswersOK(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	seedOrders(t, f, 3, "ORD-GARBAGE")

	for _, query := range []string{
		"?page=0", "?page=-3", "?page=abc", "?page=", "?page=1.5",
		"?page=99999999999999999999",
		"?page_size=0", "?page_size=abc", "?page_size=-1",
		"?page=-1&page_size=-1", "?page=abc&page_size=abc",
	} {
		for _, path := range []string{"/api/v1/admin/orders", "/api/v1/admin/attendees", "/api/v1/admin/fees"} {
			rec, body := getPage(t, e, path+query)
			require.Equal(t, http.StatusOK, rec.Code, "%s%s", path, query)
			assert.GreaterOrEqual(t, body.Page, 1, "%s%s", path, query)
			assert.GreaterOrEqual(t, body.PageSize, 1, "%s%s", path, query)
		}
	}
}

func TestAnOversizedPageSizeIsCappedRatherThanRefused(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	seedOrders(t, f, 3, "ORD-CAP-HTTP")

	rec, body := getPage(t, e, "/api/v1/admin/orders?page_size=5000")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, httpx.MaxPageSize, body.PageSize)
}

func TestAPageBeyondTheEndIsServedAsTheLastPage(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	seedOrders(t, f, 5, "ORD-BEYOND")

	rec, body := getPage(t, e, "/api/v1/admin/orders?page=999&page_size=2")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 3, body.Page, "the response reports the page it actually served")
	assert.Len(t, body.Items, 1)
}

// --- Filter input is still refused ------------------------------------------

func TestAMalformedStatusIsStillRejected(t *testing.T) {
	e, _ := newAdminOrderAPI(t)

	rec, _ := getPage(t, e, "/api/v1/admin/orders?status=NONSENSE&page=1")

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"a filter naming something that does not exist is an error, unlike a drifted page")
}

func TestAMalformedEventIdIsStillRejected(t *testing.T) {
	e, _ := newAdminOrderAPI(t)

	rec, _ := getPage(t, e, "/api/v1/admin/orders?event_id=not-a-uuid")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFiltersAndPagingCombine(t *testing.T) {
	e, f := newAdminOrderAPI(t)
	seedOrders(t, f, 4, "ORD-COMBO")
	pending := testsupport.SeedOrder(t, f.pool, "ORD-COMBO-P", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pending.ID, f.reg.ID, 1)

	rec, body := getPage(t, e, "/api/v1/admin/orders?status=PAID&page=2&page_size=3")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.EqualValues(t, 4, body.Total, "the total reflects the filter, not the table")
	assert.Len(t, body.Items, 1)
}
