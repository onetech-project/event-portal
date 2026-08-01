package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// orderNumberAttempts bounds the retry loop for order-number collisions. The
// suffix has ~1e9 combinations per day, so needing even a second attempt is
// already vanishingly unlikely; the bound exists so a pathological failure surfaces
// instead of spinning.
const orderNumberAttempts = 5

// Service implements guest checkout.
type Service struct {
	pool    db.Beginner
	repo    *Repository
	events  EventProvider
	gateway PaymentGateway
	log     *logger.Logger
	now     func() time.Time
}

// NewService builds the order service.
func NewService(pool db.Beginner, repo *Repository, events EventProvider, gateway PaymentGateway, log *logger.Logger) *Service {
	return &Service{
		pool:    pool,
		repo:    repo,
		events:  events,
		gateway: gateway,
		log:     log,
		now:     time.Now,
	}
}

// reservedLine records what TX1 actually reserved, so the compensation path knows
// exactly how much quota to give back.
type reservedLine struct {
	TicketTypeID uuid.UUID
	Name         string
	Quantity     int32
	UnitPrice    decimal.Decimal
}

// Checkout runs the three-step purchase sequence required by
// research.md "Checkout transaction shape":
//
//	TX1  quota deduction + orders + order_items + attendees, committed atomically
//	     (ARCHITECTURE.md §3.4, Constitution Principle IV)
//	 →   Gateway.CreateTransaction, outside any transaction
//	TX2  stamp payment_url / payment_provider
//
// The gateway call is deliberately outside TX1: the quota-deducting UPDATE holds a
// row lock until commit, so a ~200-500ms provider round-trip inside it would
// serialize every concurrent buyer of the same ticket type. If that call fails, a
// compensating transaction cancels the order and restores its quota so the guest
// can safely retry (spec FR-021).
func (s *Service) Checkout(ctx context.Context, req CheckoutRequest) (OrderResponse, error) {
	// Reject what can be decided without I/O first, so a malformed request never
	// opens a transaction or takes a quota row lock.
	if err := req.Validate(); err != nil {
		return OrderResponse{}, err
	}

	created, total, reserved, err := s.reserve(ctx, req)
	if err != nil {
		return OrderResponse{}, err
	}

	session, err := s.gateway.CreateTransaction(ctx, s.paymentRequestFor(created, total, reserved, req))
	if err != nil {
		s.log.ErrorContext(ctx, "payment initiation failed; compensating",
			"order_number", created.OrderNumber, "provider", s.gateway.Name(), "error", err.Error())
		s.compensate(ctx, created, reserved)
		return OrderResponse{}, apperr.Wrap(err, 502, apperr.CodePaymentInitiationFailed,
			"We could not start the payment with the provider. No tickets were reserved — please try again.")
	}

	if err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.UpdatePaymentDetails(ctx, tx, created.ID, PaymentDetails{
			// The provider's hosted QR image is recorded for audit; the guest is
			// shown an image we render ourselves from QRString.
			PaymentURL: session.QRImageURL,
			Provider:   s.gateway.Name(),
			QRString:   session.QRString,
			ExpiresAt:  session.ExpiresAt,
		})
	}); err != nil {
		// The payment session exists but we could not record it. Compensating here
		// would strand a live payment session against a cancelled order, so the
		// order is left PENDING and the guest is asked to retry; the webhook still
		// resolves it either way.
		s.log.ErrorContext(ctx, "could not persist payment details",
			"order_number", created.OrderNumber, "error", err.Error())
		return OrderResponse{}, apperr.Wrap(err, 502, apperr.CodePaymentInitiationFailed,
			"We could not complete the payment setup. Please try again.")
	}

	s.log.InfoContext(ctx, "checkout completed",
		"order_number", created.OrderNumber, "provider", s.gateway.Name(), "total_amount", total.String())

	return OrderResponse{
		OrderNumber: created.OrderNumber,
		Status:      created.Status,
		TotalAmount: money.From(total),
		// Retained for compatibility and audit. The client no longer navigates
		// here: it routes in-app to the order page, which renders the QR itself
		// (spec FR-009).
		PaymentURL: session.QRImageURL,
	}, nil
}

// reserve runs TX1, retrying only on an order-number collision.
func (s *Service) reserve(ctx context.Context, req CheckoutRequest) (OrderRecord, decimal.Decimal, []reservedLine, error) {
	var lastErr error
	for attempt := range orderNumberAttempts {
		created, total, reserved, err := s.reserveOnce(ctx, req)
		if err == nil {
			return created, total, reserved, nil
		}
		if !errors.Is(err, ErrOrderNumberTaken) {
			return OrderRecord{}, decimal.Zero, nil, err
		}
		lastErr = err
		s.log.WarnContext(ctx, "order number collision; retrying", "attempt", attempt+1)
	}
	return OrderRecord{}, decimal.Zero, nil, fmt.Errorf("could not allocate a unique order number: %w", lastErr)
}

func (s *Service) reserveOnce(ctx context.Context, req CheckoutRequest) (OrderRecord, decimal.Decimal, []reservedLine, error) {
	orderNumber, err := GenerateOrderNumber(s.now())
	if err != nil {
		return OrderRecord{}, decimal.Zero, nil, err
	}

	var (
		created  OrderRecord
		total    decimal.Decimal
		reserved []reservedLine
	)

	txErr := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Reset per attempt: a retried transaction must not accumulate state.
		total = decimal.Zero
		reserved = reserved[:0]

		for _, item := range req.Items {
			info, err := s.events.TicketTypeForCheckout(ctx, tx, item.TicketTypeID)
			if err != nil {
				return err
			}

			now := s.now()
			if now.Before(info.SalesStart) || now.After(info.SalesEnd) {
				return apperr.BadRequest(apperr.CodeTicketTypeNotOnSale,
					fmt.Sprintf("Ticket type %q is not currently on sale.", info.Name))
			}

			if err := s.events.CheckAndDeductQuota(ctx, tx, item.TicketTypeID, item.Quantity); err != nil {
				if errors.Is(err, ErrInsufficientQuota) {
					return apperr.BadRequest(apperr.CodeInsufficientQuota,
						fmt.Sprintf("Only fewer than %d ticket(s) remain for %q.", item.Quantity, info.Name))
				}
				return err
			}

			// The total is recomputed from current server-side prices; the client
			// never sends one (FR-006).
			total = total.Add(info.Price.Mul(decimal.NewFromInt32(item.Quantity)))
			reserved = append(reserved, reservedLine{
				TicketTypeID: item.TicketTypeID,
				Name:         info.Name,
				Quantity:     item.Quantity,
				UnitPrice:    info.Price,
			})
		}

		created, err = s.repo.CreateOrder(ctx, tx, CreateOrderParams{
			OrderNumber: orderNumber,
			BuyerName:   req.BuyerName,
			BuyerEmail:  req.BuyerEmail,
			BuyerPhone:  req.BuyerPhone,
			TotalAmount: total,
		})
		if err != nil {
			return err
		}

		for _, line := range reserved {
			if err := s.repo.CreateOrderItem(ctx, tx, created.ID, line.TicketTypeID, line.Quantity, line.UnitPrice); err != nil {
				return err
			}
		}

		for _, attendee := range req.Attendees {
			if _, err := s.repo.CreateAttendee(ctx, tx, created.ID, attendee.TicketTypeID, attendee.Name, attendee.Email); err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return OrderRecord{}, decimal.Zero, nil, txErr
	}

	return created, total, reserved, nil
}

// compensate undoes a committed TX1 after the gateway call failed: the order is
// cancelled and every reserved seat is returned, in one transaction (spec FR-021).
func (s *Service) compensate(ctx context.Context, created OrderRecord, reserved []reservedLine) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cancelled, err := s.repo.UpdateOrderStatusIfPending(ctx, tx, created.ID, "CANCELLED")
		if err != nil {
			return err
		}
		if !cancelled {
			// Something else already moved the order on (a webhook that beat us
			// here). Its quota accounting is that path's responsibility, not ours.
			return nil
		}
		for _, line := range reserved {
			if err := s.events.RestoreQuota(ctx, tx, line.TicketTypeID, line.Quantity); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Nothing more can be done in-band. This is logged loudly because it is the
		// one path that can leave quota held by an order that will never be paid.
		s.log.ErrorContext(ctx, "compensation failed; quota may remain reserved",
			"order_number", created.OrderNumber, "order_id", created.ID.String(), "error", err.Error())
	}
}

func (s *Service) paymentRequestFor(created OrderRecord, total decimal.Decimal, reserved []reservedLine, req CheckoutRequest) PaymentRequest {
	items := make([]PaymentItem, 0, len(reserved))
	for _, line := range reserved {
		items = append(items, PaymentItem{
			ID:       line.TicketTypeID.String(),
			Name:     line.Name,
			Price:    line.UnitPrice,
			Quantity: line.Quantity,
		})
	}
	return PaymentRequest{
		OrderNumber:   created.OrderNumber,
		GrossAmount:   total,
		CustomerName:  req.BuyerName,
		CustomerEmail: req.BuyerEmail,
		CustomerPhone: req.BuyerPhone,
		Items:         items,
	}
}
