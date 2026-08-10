package payment_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// --- T018: the sweeper covers the 1-hour booking hold and logs FR-009 -------

// newSweepFixture builds a payment service whose logs are captured, plus a
// booked-shape PENDING order (buyer NULL, no payment fields) holding 2 seats
// whose deadline has already passed.
func newSweepFixture(t *testing.T) (*payment.Service, *bytes.Buffer, *testsupport.Pool, testsupport.Order, testsupport.TicketType) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	ev := testsupport.SeedEvent(t, pool, "held-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 8)

	// The booked shape: PENDING, buyer columns NULL, payment fields NULL — only
	// the hold deadline set, already in the past.
	var ord testsupport.Order
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO orders (order_number, total_amount, status_id, payment_expires_at)
		VALUES ('ORD-HOLD-SWEEP', 200000,
		        (SELECT id FROM order_statuses WHERE name = 'PENDING'),
		        now() - interval '1 minute')
		RETURNING id`).Scan(&ord.ID))
	ord.OrderNumber = "ORD-HOLD-SWEEP"
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 2)
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "", "")

	logBuf := &bytes.Buffer{}
	svc := payment.NewService(
		pool,
		payment.NewRepository(pool),
		&stubGateway{},
		orderAdapter{repo: order.NewRepository(pool)},
		quotaAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		reserverAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		orderAdapter{repo: order.NewRepository(pool)},
		&spyFulfiller{},
		&spyFulfiller{},
		logger.NewWithWriter(logBuf, logger.LevelInfo),
	)
	return svc, logBuf, pool, ord, tt
}

func TestSweeperExpiresTheBookingHoldAndRestoresQuota(t *testing.T) {
	svc, _, pool, ord, tt := newSweepFixture(t)

	expired, err := svc.ExpireDueOrders(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, expired, 1)

	// The hold expired through the same path as the payment window: same
	// payment_expires_at column, same sweeper, no new job (booking-flow.md §6).
	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, pool, ord.ID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, pool, tt.ID),
		"the 2 held seats return to the pool")
}

func TestSweeperLogsExpiredOrderIdentityAndRestoredQuota(t *testing.T) {
	svc, logBuf, _, ord, tt := newSweepFixture(t)

	_, err := svc.ExpireDueOrders(context.Background())
	require.NoError(t, err)

	// FR-009: the expiry is auditable from the logs alone — which order, and
	// exactly which quota lines went back.
	logs := logBuf.String()
	assert.Contains(t, logs, "order expired at its payment deadline")
	assert.Contains(t, logs, ord.OrderNumber)
	assert.Contains(t, logs, "restored_quota")
	assert.Contains(t, logs, fmt.Sprintf("%s:+2", tt.ID))
}
