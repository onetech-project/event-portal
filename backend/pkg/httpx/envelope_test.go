package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func TestRespondWrapsDataInEnvelope(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)

	err := httpx.Respond(c, http.StatusCreated, map[string]string{"order_id": "ORD-1"})

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, `{"code":200000,"message":"Success","data":{"order_id":"ORD-1"}}`, rec.Body.String())
}

func TestRespondNullDataStaysExplicit(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)

	require.NoError(t, httpx.Respond(c, http.StatusOK, nil))

	assert.JSONEq(t, `{"code":200000,"message":"Success","data":null}`, rec.Body.String())
}
