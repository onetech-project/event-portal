package httpx

import "github.com/labstack/echo/v4"

// SuccessCode is the numeric code carried by every successful response
// (clarification 2026-08-05: the whole API speaks the {code, message, data}
// envelope; success is always 200000 regardless of the 2xx status line).
const SuccessCode = 200000

// Envelope is the one JSON shape every endpoint returns. Data is the typed DTO
// for successes; for errors it is nil except where a contract attaches detail
// (a validation field map, the current QR payload on PAYMENT_ALREADY_STARTED).
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// Respond writes a success envelope around data with the given HTTP status.
func Respond(c echo.Context, status int, data any) error {
	return c.JSON(status, Envelope{Code: SuccessCode, Message: "Success", Data: data})
}
