package payment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// Everything here runs against an httptest stub. That is not a concession for
// the tests' benefit — pointing PG_BASE_URL at a stub is the ordinary way to run
// this system offline (FR-008a), so these exercise the same path a developer
// without gateway credentials uses.

const sampleQR = "00020101021226620015ID.CO.MANJO.WWW01189360085801764127110210MT6016911703" +
	"03UME51470014ID.CO.QRIS.WWW0209AYOBORONG04121.0.10.08.265204723053033605408100" +
	"00.00550202560410005802ID5909Ayoborong6013JAKARTA PUSAT61051011062520520A48591" +
	"41800854720AA307034270817Payment for goods6304EB21"

// capturedRequest is what the stub saw, so a test can assert on the wire shape
// rather than on our own struct.
type capturedRequest struct {
	body   map[string]any
	header http.Header
}

type stubServer struct {
	*httptest.Server
	requests []capturedRequest
}

// newStubGatewayServer answers every session-open with the given handler,
// recording what it received.
func newStubGatewayServer(t *testing.T, handler func(attempt int, w http.ResponseWriter)) *stubServer {
	t.Helper()
	stub := &stubServer{}
	var attempts int32

	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		stub.requests = append(stub.requests, capturedRequest{body: body, header: r.Header.Clone()})

		assert.Equal(t, "/v1/manjo/transaction/incoming", r.URL.Path)
		handler(int(atomic.AddInt32(&attempts, 1)), w)
	}))
	t.Cleanup(stub.Close)
	return stub
}

// respondJSON is the common stub reply.
func respondJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

// successBody builds a session-open success with the given qr_ea literal. Pass
// an empty string to omit the field entirely.
func successBody(expiredAt string) string {
	mr := fmt.Sprintf(`{"qr_r":%q,"qr_u":""`, sampleQR)
	if expiredAt != "" {
		mr += `,"qr_ea":` + expiredAt
	}
	mr += "}"
	return `{"s":true,"e":null,"d":{"m":0,"eri":"A485931237885664E26C","mr":` + mr + `}}`
}

func newTestGateway(baseURL string, window time.Duration) *payment.ManjoGateway {
	return payment.NewManjoGateway(payment.ManjoConfig{
		BaseURL:        baseURL,
		ServerKey:      "the-server-key",
		ClientKey:      "the-client-key",
		CallbackToken:  "the-callback-token",
		ExpectedWindow: window,
		HTTPClient:     &http.Client{Timeout: 2 * time.Second},
		Log:            testsupport.DiscardLogger(),
	})
}

func sampleTransaction() payment.TransactionRequest {
	return payment.TransactionRequest{
		OrderNumber:  "ORD-20260810-A7K2QX",
		GrossAmount:  decimal.RequireFromString("117750.00"),
		CustomerName: "Budi Santoso",
		// Present on the domain shape, with no destination in this contract.
		CustomerEmail: "budi@example.com",
		CustomerPhone: "+628123456789",
		Items: []payment.TransactionItem{
			{ID: "t1", Name: "Regular", Price: decimal.NewFromInt(100000), Quantity: 1},
		},
	}
}

// --- Request shape (T021–T023) --------------------------------------------

// Exactly six required fields plus the optional payer name — and no requested
// expiry, because the gateway applies and reports its own (FR-002, FR-002c).
func TestSessionOpenSendsTheRequiredFieldsAndNoExpiry(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, successBody(`"2026-08-10T10:26:46+07:00"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	_, err := gw.CreateTransaction(context.Background(), sampleTransaction())
	require.NoError(t, err)

	require.Len(t, stub.requests, 1)
	body := stub.requests[0].body

	assert.Equal(t, "ORD-20260810-A7K2QX", body["ri"])
	assert.InDelta(t, 117750.0, body["a"], 0.001)
	assert.Equal(t, "IDR", body["c"])
	assert.InDelta(t, 0.0, body["m"], 0.001, "0 is the QR method")
	assert.NotEmpty(t, body["r"])
	assert.Equal(t, "Budi Santoso", body["pn"])

	credential := body["ac"].(map[string]any)["cr"].(map[string]any)
	assert.Equal(t, "the-client-key", credential["client_id"])
	assert.Equal(t, "the-server-key", credential["client_secret"])

	_, hasExpiry := body["e"]
	assert.False(t, hasExpiry, "no expiry may be requested — the gateway owns the deadline (FR-002c)")
}

// The credential travels in the body, which means it must not also travel in a
// header where a proxy log would capture it.
func TestSessionOpenSendsNoAuthorizationHeader(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, successBody(`"2026-08-10T10:26:46+07:00"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	_, err := gw.CreateTransaction(context.Background(), sampleTransaction())
	require.NoError(t, err)

	assert.Empty(t, stub.requests[0].header.Get("Authorization"))
}

// FR-002b: the frozen order total goes out unchanged. Recomputing at request
// time would let the figure in the code disagree with the one the guest agreed
// to, and the callback carries no amount to catch it with.
func TestSessionOpenSendsTheFrozenTotalUnchanged(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, successBody(`"2026-08-10T10:26:46+07:00"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	req := sampleTransaction()
	req.GrossAmount = decimal.RequireFromString("11550.11")

	_, err := gw.CreateTransaction(context.Background(), req)
	require.NoError(t, err)

	assert.InDelta(t, 11550.11, stub.requests[0].body["a"], 0.001,
		"the amount is sent as frozen, not rounded or truncated on our side")
}

// FR-010a: refuse, never truncate. A truncated reference comes back on the
// callback matching no order, turning a caught misconfiguration into a payment
// nobody can attribute.
func TestSessionOpenRefusesAnOverLongReference(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		t.Error("an over-long reference must never reach the gateway")
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	req := sampleTransaction()
	req.OrderNumber = strings.Repeat("X", 26)

	_, err := gw.CreateTransaction(context.Background(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "25")
	assert.Empty(t, stub.requests)
}

func TestSessionOpenAcceptsAReferenceAtTheLimit(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, successBody(`"2026-08-10T10:26:46+07:00"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	req := sampleTransaction()
	req.OrderNumber = strings.Repeat("X", 25)

	_, err := gw.CreateTransaction(context.Background(), req)
	require.NoError(t, err)
}

// --- Response parsing (T024) ----------------------------------------------

func TestSessionOpenMapsTheSuccessResponse(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, successBody(`"2026-08-10T10:26:46+07:00"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
	require.NoError(t, err)

	assert.Equal(t, "A485931237885664E26C", session.ProviderRef)
	assert.Equal(t, sampleQR, session.QRString)
	assert.Empty(t, session.QRImageURL)
	assert.Empty(t, session.RedirectURL, "QRIS never redirects")
}

// Only `s: true` is a session — whatever the HTTP status said (FR-003).
func TestSessionOpenTreatsSuccessFalseAsAFailure(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK, `{"s":false,"e":"merchant not active","d":{}}`)
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	_, err := gw.CreateTransaction(context.Background(), sampleTransaction())

	require.Error(t, err)
	assert.NotErrorIs(t, err, payment.ErrDuplicateReference)
}

// FR-006: without a payload there is nothing to render, and a payment page with
// no code is worse than a failed checkout — failing releases the seats.
func TestSessionOpenTreatsAnEmptyQRAsAFailure(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusOK,
			`{"s":true,"e":null,"d":{"m":0,"eri":"E1","mr":{"qr_r":"","qr_u":""}}}`)
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	_, err := gw.CreateTransaction(context.Background(), sampleTransaction())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no QR payload")
}

// --- Expiry handling (T025–T028) ------------------------------------------

// The four cases FR-009 distinguishes. They share an outcome in three of the
// four, which is exactly why they must stay distinguishable in the logs.
func TestSessionOpenExpiryHandling(t *testing.T) {
	const window = 15 * time.Minute

	t.Run("a usable expiry is adopted verbatim", func(t *testing.T) {
		want := time.Now().Add(9 * time.Minute).Truncate(time.Second)
		stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
			respondJSON(w, http.StatusOK, successBody(`"`+want.Format(time.RFC3339)+`"`))
		})
		gw := newTestGateway(stub.URL, window)

		session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
		require.NoError(t, err)

		assert.True(t, session.ExpiryFromGateway)
		assert.WithinDuration(t, want, session.ExpiresAt, time.Second,
			"a 15-minute countdown here would mean qr_ea was dropped — the silent failure R1 warns about")
	})

	t.Run("an absent expiry falls back and is flagged", func(t *testing.T) {
		stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
			respondJSON(w, http.StatusOK, successBody(""))
		})
		gw := newTestGateway(stub.URL, window)

		session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
		require.NoError(t, err)

		assert.False(t, session.ExpiryFromGateway, "the caller must be able to tell this was a fallback")
		assert.WithinDuration(t, time.Now().Add(window), session.ExpiresAt, 5*time.Second)
		// The trap the timestamp type introduces: an omitted qr_ea decodes to a
		// VALID year-1 time, not to nothing. Treated as an ordinary expiry it reads
		// as "already expired" — right fallback, wrong reason, and the signal lost.
		assert.True(t, session.ExpiresAt.After(time.Now()),
			"a year-1 deadline means the zero value was treated as a real expiry")
	})

	t.Run("an expiry already in the past falls back", func(t *testing.T) {
		past := time.Now().Add(-time.Hour).Format(time.RFC3339)
		stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
			respondJSON(w, http.StatusOK, successBody(`"`+past+`"`))
		})
		gw := newTestGateway(stub.URL, window)

		session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
		require.NoError(t, err)

		assert.False(t, session.ExpiryFromGateway)
		assert.True(t, session.ExpiresAt.After(time.Now()))
	})

	// FR-009e. An unreadable timestamp is a deadline problem; it must never be
	// the reason a guest is denied a code that decoded perfectly well.
	t.Run("an unreadable expiry still yields a usable code", func(t *testing.T) {
		stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
			respondJSON(w, http.StatusOK, successBody(`"tomorrow"`))
		})
		gw := newTestGateway(stub.URL, window)

		session, err := gw.CreateTransaction(context.Background(), sampleTransaction())

		require.NoError(t, err, "the decode error must not take the payload down with the timestamp")
		assert.Equal(t, sampleQR, session.QRString)
		assert.False(t, session.ExpiryFromGateway)
		assert.WithinDuration(t, time.Now().Add(window), session.ExpiresAt, 5*time.Second)
	})

	// FR-009c: adopted as-is, not capped. A shorter deadline of our own would
	// strand payments in the gap; a longer one would outlive the code.
	t.Run("an expiry far longer than expected is adopted, not capped", func(t *testing.T) {
		want := time.Now().Add(4 * time.Hour).Truncate(time.Second)
		stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
			respondJSON(w, http.StatusOK, successBody(`"`+want.Format(time.RFC3339)+`"`))
		})
		gw := newTestGateway(stub.URL, window)

		session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
		require.NoError(t, err)

		assert.True(t, session.ExpiryFromGateway)
		assert.WithinDuration(t, want, session.ExpiresAt, time.Second)
	})

	// FR-009a: the instant is what matters, not the wall-clock reading. The same
	// moment expressed in two offsets must produce one deadline.
	t.Run("the instant survives the offset", func(t *testing.T) {
		// Must be in the future, or the already-past branch would answer instead
		// and both offsets would agree on the fallback rather than on the instant.
		base := time.Now().Add(12 * time.Minute).UTC().Truncate(time.Second)
		for _, literal := range []string{
			base.In(time.FixedZone("WIB", 7*3600)).Format(time.RFC3339),
			base.Format(time.RFC3339),
		} {
			stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
				respondJSON(w, http.StatusOK, successBody(`"`+literal+`"`))
			})
			gw := newTestGateway(stub.URL, window)

			session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
			require.NoError(t, err, literal)
			assert.True(t, session.ExpiresAt.Equal(base), "%s must denote the same instant", literal)
		}
	})
}

// --- Retry and duplicate reference (T029, T030 / T034) --------------------

// FR-007a–c: a one-second blip must not fail a checkout.
func TestSessionOpenRetriesATransientFailureUnderTheSameReference(t *testing.T) {
	stub := newStubGatewayServer(t, func(attempt int, w http.ResponseWriter) {
		if attempt == 1 {
			respondJSON(w, http.StatusBadGateway, `{"s":false,"e":"upstream unavailable"}`)
			return
		}
		respondJSON(w, http.StatusOK, successBody(`"`+time.Now().Add(15*time.Minute).Format(time.RFC3339)+`"`))
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	session, err := gw.CreateTransaction(context.Background(), sampleTransaction())
	require.NoError(t, err)
	assert.Equal(t, sampleQR, session.QRString)

	require.Len(t, stub.requests, 2)
	// FR-007b. Reusing the reference is what makes retrying safe at all: the
	// gateway refuses a reference it has already issued a code for, so a retry
	// can never produce a second live code for one order.
	assert.Equal(t, stub.requests[0].body["ri"], stub.requests[1].body["ri"],
		"a retry must reuse the reference unchanged, never mint a new one")
}

func TestSessionOpenGivesUpAfterItsRetryBudget(t *testing.T) {
	stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
		respondJSON(w, http.StatusInternalServerError, `{"s":false,"e":"boom"}`)
	})
	gw := newTestGateway(stub.URL, 15*time.Minute)

	started := time.Now()
	_, err := gw.CreateTransaction(context.Background(), sampleTransaction())

	require.Error(t, err)
	assert.Len(t, stub.requests, 3, "one attempt plus two retries")
	// FR-007c: bounded by a guest's patience, explicitly NOT the gateway's own
	// ten-second pacing — correct for a background callback, unacceptable here.
	assert.Less(t, time.Since(started), 10*time.Second)
}

// FR-007d. Terminal, and distinct from a generic failure: a code exists for this
// reference that this system will never hold, because no call returns an
// existing one. Retrying cannot help.
func TestDuplicateReferenceIsTerminalAndNotRetried(t *testing.T) {
	for _, detail := range []string{
		"duplicate ref id",
		"REF ID EXIST",
	} {
		t.Run(detail, func(t *testing.T) {
			stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
				respondJSON(w, http.StatusOK,
					fmt.Sprintf(`{"s":false,"e":%q,"d":{}}`, detail))
			})
			gw := newTestGateway(stub.URL, 15*time.Minute)

			_, err := gw.CreateTransaction(context.Background(), sampleTransaction())

			require.ErrorIs(t, err, payment.ErrDuplicateReference)
			assert.Len(t, stub.requests, 1, "a duplicate reference must not be retried")
		})
	}
}

// A generic refusal must NOT be mistaken for a duplicate reference: the two
// need opposite handling, and releasing an order's seats on an ordinary failure
// would turn a retryable blip into a lost booking.
//
// `MERCHANT_NOT_AVAILABLE` is here because it was once mistaken for exactly
// that, and the mistake took every checkout down: the gateway ran out of
// available merchants, every guest's order was cancelled on arrival, and "please
// book again" failed identically. It means what it says — no merchant to route
// to — and it is a transient operational failure, not a statement about our
// reference. See [research.md R11].
func TestAGenericRefusalIsNotTreatedAsADuplicate(t *testing.T) {
	for _, detail := range []string{"INVALID_AMOUNT", "MERCHANT_NOT_AVAILABLE"} {
		t.Run(detail, func(t *testing.T) {
			stub := newStubGatewayServer(t, func(_ int, w http.ResponseWriter) {
				respondJSON(w, http.StatusOK, fmt.Sprintf(`{"s":false,"e":%q,"d":{}}`, detail))
			})
			gw := newTestGateway(stub.URL, 15*time.Minute)

			_, err := gw.CreateTransaction(context.Background(), sampleTransaction())

			require.Error(t, err)
			assert.NotErrorIs(t, err, payment.ErrDuplicateReference,
				"the order must stay PENDING with its seats held so the guest can retry (FR-007)")
		})
	}
}

// --- Callback authentication (T036, T037) ---------------------------------

func TestVerifyWebhookAcceptsTheConfiguredToken(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)
	payload := []byte(`{"ri":"ORD-20260810-A7K2QX","nti":"A48593","s":5,"td":"2026-08-10T10:15:22+07:00","tt":0}`)

	result, err := gw.VerifyWebhook(payload, "the-callback-token")
	require.NoError(t, err)

	assert.Equal(t, "ORD-20260810-A7K2QX", result.OrderNumber)
	assert.Equal(t, "A48593", result.TransactionID)
	assert.Equal(t, "Completed", result.TransactionStatus)
	assert.True(t, result.StatusPresent)
	assert.True(t, result.IsDeposit)
	assert.Equal(t, payload, result.RawPayload)
}

func TestVerifyWebhookRefusesAMissingOrWrongToken(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)
	payload := []byte(`{"ri":"ORD-1","s":5,"tt":0}`)

	for _, token := range []string{"", "wrong", "the-callback-token "} {
		_, err := gw.VerifyWebhook(payload, token)
		assert.ErrorIs(t, err, payment.ErrInvalidSignature, "token %q", token)
	}
}

// A body this system cannot parse is a body it cannot authenticate.
func TestVerifyWebhookRefusesAnUnreadableBody(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)

	_, err := gw.VerifyWebhook([]byte(`not json`), "the-callback-token")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRefusesANotificationWithNoReference(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)

	_, err := gw.VerifyWebhook([]byte(`{"s":5,"tt":0}`), "the-callback-token")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

// Pending is the enum's zero value, so a body omitting `s` decodes as "pending"
// rather than erroring. The outcome is the same either way, but the audit record
// must not claim the gateway said "pending" when it said nothing at all.
func TestVerifyWebhookDistinguishesAnAbsentStatusFromAnExplicitPending(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)

	absent, err := gw.VerifyWebhook([]byte(`{"ri":"ORD-1","tt":0}`), "the-callback-token")
	require.NoError(t, err)
	assert.False(t, absent.StatusPresent)
	assert.Equal(t, "ABSENT", absent.TransactionStatus)

	explicit, err := gw.VerifyWebhook([]byte(`{"ri":"ORD-1","s":0,"tt":0}`), "the-callback-token")
	require.NoError(t, err)
	assert.True(t, explicit.StatusPresent)
	assert.Equal(t, "Pending", explicit.TransactionStatus)
}

// FR-020: a withdrawal is not a payment for one of our orders.
func TestVerifyWebhookFlagsANonDeposit(t *testing.T) {
	gw := newTestGateway("http://unused", 15*time.Minute)

	result, err := gw.VerifyWebhook([]byte(`{"ri":"ORD-1","s":5,"tt":1}`), "the-callback-token")
	require.NoError(t, err)

	assert.False(t, result.IsDeposit)
	assert.Equal(t, "WITHDRAW", result.TransactionType)
}

// An empty configured token must not authenticate an empty presented one.
// Configuration refuses to start without it, so this is the second line.
func TestVerifyWebhookRefusesWhenNoTokenIsConfigured(t *testing.T) {
	gw := payment.NewManjoGateway(payment.ManjoConfig{
		BaseURL: "http://unused",
		Log:     testsupport.DiscardLogger(),
	})

	_, err := gw.VerifyWebhook([]byte(`{"ri":"ORD-1","s":5,"tt":0}`), "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestGatewayNameIsManjo(t *testing.T) {
	assert.Equal(t, "manjo", newTestGateway("http://unused", time.Minute).Name())
}
