package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/db"
)

// registrationFulfillmentTimeout bounds the post-commit goroutine: ticket
// generation plus PDF render plus SMTP. Generous, because a slow mail server is
// not a reason to abandon a ticket the guest has already been told about.
const registrationFulfillmentTimeout = 2 * time.Minute

// registrationRefusal is the ONE sentence every unavailable-registration answer
// carries. Six distinct causes share it deliberately — see refuseRegistration.
const registrationRefusal = "This registration is not available."

// WithFulfillment installs the post-commit issuance and delivery seams for free
// registration (spec 022).
//
// Two interfaces rather than one because the two failures are different: without
// tickets there is nothing to email and the flow must stop, whereas a delivery
// failure leaves valid tickets and an armed resend. That is the same split, and
// the same reasoning, as the payment webhook's fulfillAsync.
func (s *Service) WithFulfillment(issuer TicketIssuer, deliverer TicketDeliverer) *Service {
	s.issuer = issuer
	s.deliverer = deliverer
	return s
}

// WaitForRegistrationFulfillment blocks until every in-flight registration
// fulfilment goroutine has finished. The composition root calls it during
// graceful shutdown so a registration accepted moments before SIGTERM still
// issues and delivers.
func (s *Service) WaitForRegistrationFulfillment() { s.fulfillment.Wait() }

// RegistrationPrerequisites answers GET /ticket/register/:ticket_type_id: whether
// this registration is open, and what the form needs to render (FR-010, FR-011).
//
// Every refusal collapses to one code and one sentence (FR-012). The real reason
// is logged, not returned.
func (s *Service) RegistrationPrerequisites(ctx context.Context, slug string, ticketTypeID uuid.UUID) (RegistrationPrereqs, error) {
	target, err := s.resolveRegistrationTarget(ctx, slug, ticketTypeID)
	if err != nil {
		return RegistrationPrereqs{}, err
	}

	terms, err := s.events.CurrentTerms(ctx, target.EventID)
	if err != nil {
		if errors.Is(err, ErrNoTerms) {
			// NOT collapsed into the generic refusal. The event exists and the
			// registration is otherwise open; this says the organiser has not
			// authored terms yet, which discloses nothing an unauthenticated
			// caller cannot already learn from the booking path, which answers
			// the same code for the same condition.
			return RegistrationPrereqs{}, apperr.Conflict(apperr.CodeTermsMissing,
				"This event has no Terms & Conditions to agree to yet.")
		}
		return RegistrationPrereqs{}, err
	}

	genders, err := s.Genders(ctx)
	if err != nil {
		return RegistrationPrereqs{}, err
	}

	return RegistrationPrereqs{
		TicketTypeID:   target.TicketTypeID,
		TicketTypeName: target.TicketTypeName,
		Event: RegistrationEvent{
			ID:   target.EventID,
			Name: target.EventName,
			Slug: target.EventSlug,
		},
		EventTermsID:        terms.ID,
		EventTermsUpdatedAt: terms.UpdatedAt,
		Genders:             genders,
	}, nil
}

// activeGenders is the master the registration form validates against: the set
// of ACTIVE entry identifiers (FR-022a, FR-022b). It resolves nothing any more —
// the form submits the identifier itself (clarified 2026-08-24) — so this is
// purely a membership check.
//
// Deliberately NOT genderMaps' `known`, which is checkout's. Checkout has to
// accept a retired value because a restored booking form legitimately carries
// one that was active when it was saved (spec 011 FR-031). A registration form
// is rendered fresh from RegistrationPrerequisites on every visit and is never
// restored, so it has no such case and a retired value is refused
// (clarified 2026-08-24).
//
// Reading the same query the prerequisites call offers its options from is what
// makes the form and the validator agree by construction: the refusal can never
// fire on a value this feature itself had just displayed.
//
// The returned set is the shape Validate wants, so an absent identifier is
// reported as a field-level refusal rather than written straight through — which
// would be a gender_id referencing no row. That risk is HIGHER now that an
// identifier arrives: an unmatched name merely failed to match.
func (s *Service) activeGenders(ctx context.Context) (map[int16]struct{}, error) {
	records, err := s.activeGenderRecords(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[int16]struct{}, len(records))
	for _, r := range records {
		active[r.ID] = struct{}{}
	}
	return active, nil
}

// RegisterFree issues one free ticket (spec 022 FR-026 … FR-032).
//
//	pre-TX  validate shape · resolve target · resolve + version-check terms ·
//	        resolve gender master              (reads that can fail, kept OUT of
//	                                            the lock window)
//	TX      advisory lock on (event, email) → duplicate check → guarded quota
//	        deduction → INSERT orders (PAID, total 0, is_registration) →
//	        order_items → attendee slot → fill it
//	post    issuance + delivery, off the request path
//
// The transaction contains NO network call and NO cache command (Principle IV
// and Principle VII). The quota UPDATE holds a row lock until COMMIT, so a
// round trip inside it would serialise every concurrent registrant behind
// network latency — and `pkg/cache` refuses outright on a transaction context,
// so an inlining refactor fails loudly rather than quietly halving throughput.
func (s *Service) RegisterFree(ctx context.Context, ticketTypeID uuid.UUID, req RegistrationRequest) error {
	genders, err := s.activeGenders(ctx)
	if err != nil {
		return err
	}
	if err := req.Validate(genders); err != nil {
		return err
	}

	target, err := s.resolveRegistrationTarget(ctx, req.Slug, ticketTypeID)
	if err != nil {
		return err
	}

	terms, err := s.events.CurrentTerms(ctx, target.EventID)
	if err != nil {
		if errors.Is(err, ErrNoTerms) {
			return apperr.Conflict(apperr.CodeTermsChanged,
				"The Terms & Conditions changed. Please review the current version.")
		}
		return err
	}
	// Compare the VERSION, never the id. An admin edit is an in-place overwrite
	// that preserves the row id, so an id comparison can never detect one.
	// Truncated to the second: the value round-trips through JSON and back, and
	// sub-second drift is not a document change.
	if !terms.UpdatedAt.Truncate(time.Second).Equal(req.EventTermsUpdatedAt.UTC().Truncate(time.Second)) {
		return apperr.Conflict(apperr.CodeTermsChanged,
			"The Terms & Conditions changed while you were reading them. Please review the current version.")
	}

	email := strings.TrimSpace(req.Email)
	dob, err := time.Parse(visitorDobFormat, req.Dob)
	if err != nil {
		// Validate already proved this parses; a failure here is a programming
		// error, not a guest one.
		return fmt.Errorf("registration: dob passed validation but did not parse: %w", err)
	}

	var created OrderRecord
	for attempt := range orderNumberAttempts {
		created, err = s.registerOnce(ctx, target, terms.ID, email, dob, req.GenderID, req)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrOrderNumberTaken) {
			return err
		}
		s.log.WarnContext(ctx, "registration order number collision; retrying", "attempt", attempt+1)
	}
	if err != nil {
		return fmt.Errorf("could not allocate a unique order number: %w", err)
	}

	s.log.InfoContext(ctx, "free registration recorded",
		"order_number", created.OrderNumber,
		"event_id", target.EventID.String(),
		"ticket_type_id", target.TicketTypeID.String())

	s.fulfillRegistrationAsync(ctx, created)
	return nil
}

// registerOnce is the transaction. Split out so an order-number collision can be
// retried against a clean transaction — a unique violation aborts the current
// one in PostgreSQL, so retrying inside it is impossible.
func (s *Service) registerOnce(
	ctx context.Context,
	target RegistrationTarget,
	eventTermsID uuid.UUID,
	email string,
	dob time.Time,
	genderID int16,
	req RegistrationRequest,
) (OrderRecord, error) {
	orderNumber, err := GenerateOrderNumber(s.now())
	if err != nil {
		return OrderRecord{}, err
	}

	var created OrderRecord
	txErr := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		// The quota deduction is the FIRST statement now. No address-scoped lock
		// precedes it (FR-023a): two submissions of one email are two ordinary
		// registrations and must not contend with each other at all. The only
		// contention left is the quota row lock, which is per ticket type and
		// shared with the purchase path.
		if err := s.events.CheckAndDeductQuota(ctx, tx, target.TicketTypeID, 1); err != nil {
			if errors.Is(err, ErrInsufficientQuota) {
				return apperr.BadRequest(apperr.CodeInsufficientQuota,
					"There are no places left for this registration.")
			}
			return err
		}

		created, err = s.repo.CreateRegistrationOrder(ctx, tx, orderNumber, RegistrationBuyer{
			Name:  strings.TrimSpace(req.Name),
			Email: email,
			Phone: strings.TrimSpace(req.Phone),
		}, eventTermsID)
		if err != nil {
			return err
		}

		// Zero-priced line. No order_fees row accompanies it: fees are computed
		// from a subtotal, and a registration's is zero by construction (FR-027).
		if err := s.repo.CreateOrderItem(ctx, tx, created.ID,
			TicketLine(target.TicketTypeID), 1, decimal.Zero); err != nil {
			return err
		}

		slotID, err := s.repo.CreateAttendeeSlot(ctx, tx, created.ID,
			AttendeeRef{TicketTypeID: target.TicketTypeID})
		if err != nil {
			return err
		}
		// Filled immediately — a registration has no held phase in which the slot
		// sits empty. UpdateAttendeeDetails carries no status guard (unlike
		// UpdateOrderBuyer), so it works against an order created at PAID.
		filled, err := s.repo.UpdateAttendeeDetails(ctx, tx, slotID, created.ID, SlotDetails{
			Name:     strings.TrimSpace(req.Name),
			Email:    email,
			Phone:    strings.TrimSpace(req.Phone),
			Dob:      dob,
			GenderID: genderID,
		})
		if err != nil {
			return err
		}
		if !filled {
			return fmt.Errorf("registration: attendee slot %s vanished before it could be filled", slotID)
		}

		// Registered, not performed: the quota UPDATE above holds a row lock until
		// COMMIT, and a Redis round trip inside it would serialise every
		// concurrent registrant. Both scopes, because quota moved (event) and an
		// order appeared (orders).
		cache.InvalidateAfterCommit(ctx, s.cache, cache.Orders(), cache.Event(target.EventID))
		return nil
	})
	if txErr != nil {
		return OrderRecord{}, txErr
	}
	return created, nil
}

// fulfillRegistrationAsync issues the ticket and delivers it off the request
// path, so the guest's 201 does not wait on PDF rendering and SMTP.
//
// Mirrors payment.fulfillAsync deliberately, including WithoutCancel: it keeps
// the request's values — crucially its trace context — while dropping the
// cancellation that fires the moment the response is written. Deriving from
// context.Background() would orphan this work from the request's trace, which is
// exactly where you look when a registrant says the email never arrived.
func (s *Service) fulfillRegistrationAsync(requestCtx context.Context, ord OrderRecord) {
	if s.issuer == nil || s.deliverer == nil {
		s.log.ErrorContext(requestCtx, "registration recorded but no fulfilment seam is configured",
			"order_number", ord.OrderNumber)
		return
	}

	s.fulfillment.Add(1)
	go func() {
		defer s.fulfillment.Done()

		ctx, cancel := context.WithTimeout(
			context.WithoutCancel(requestCtx), registrationFulfillmentTimeout)
		defer cancel()

		log := s.log.With("order_number", ord.OrderNumber, "order_id", ord.ID.String())

		if err := s.issuer.IssueTicketsForOrder(ctx, ord.ID); err != nil {
			// Without a ticket there is nothing to deliver. Stopping here is what
			// keeps the guest from receiving an empty e-ticket document.
			log.ErrorContext(ctx, "ticket generation failed for a registration", "error", err.Error())
			return
		}

		if err := s.deliverer.SendTicketEmail(ctx, ord.ID); err != nil {
			// The ticket exists and is valid; only delivery failed. email_sent
			// stays false so admin resend remains armed — which for a
			// registration is the ONLY recovery route, because the confirmation
			// page shows the guest no address and no reference (FR-038, FR-039e).
			log.ErrorContext(ctx, "ticket email delivery failed for a registration", "error", err.Error())
			return
		}
	}()
}

// resolveRegistrationTarget resolves and gates the registration link, collapsing
// every "you cannot register here" cause onto one indistinguishable refusal.
func (s *Service) resolveRegistrationTarget(ctx context.Context, slug string, ticketTypeID uuid.UUID) (RegistrationTarget, error) {
	target, err := s.events.RegistrationTargetBySlug(ctx, strings.TrimSpace(slug), ticketTypeID)
	if err != nil {
		if errors.Is(err, ErrRegistrationTargetMissing) {
			return RegistrationTarget{}, s.refuseRegistration(ctx, slug, ticketTypeID, "no such event or ticket type")
		}
		return RegistrationTarget{}, err
	}

	// The boundary that stops this path from minting paid tickets. Hiding the
	// form is not enforcement — this refusal must hold for a caller who never
	// loaded the page (US3 scenario 2).
	// VISIBLE means purchasable, which is exactly what this endpoint must not
	// accept. The mirror image of the demand.go seam refusal.
	if target.IsVisible {
		return RegistrationTarget{}, s.refuseRegistration(ctx, slug, ticketTypeID, "ticket type is purchasable, not registration-only")
	}

	now := s.now()
	if now.Before(target.SalesStart) {
		return RegistrationTarget{}, s.refuseRegistration(ctx, slug, ticketTypeID, "registration window has not opened")
	}
	if now.After(target.SalesEnd) {
		return RegistrationTarget{}, s.refuseRegistration(ctx, slug, ticketTypeID, "registration window has closed")
	}
	// A display-only read, exactly as the field's contract requires: it makes an
	// exhausted link say so instead of presenting a form that cannot succeed. It
	// authorises nothing — the guarded UPDATE inside the transaction is the only
	// authority on whether a place exists (Principle VII).
	if target.QuotaRemaining <= 0 {
		return RegistrationTarget{}, s.refuseRegistration(ctx, slug, ticketTypeID, "no places remain")
	}

	return target, nil
}

// refuseRegistration answers every unavailable cause identically.
//
// Collapsing six causes onto one code and one sentence is deliberate: FR-012
// forbids disclosing which ticket types or events exist, and a distinct code per
// cause is an enumeration oracle. The cost — an operator debugging a broken link
// learns nothing from the response — is paid on this side of the wire, in the
// log line, where it costs an attacker nothing.
func (s *Service) refuseRegistration(ctx context.Context, slug string, ticketTypeID uuid.UUID, reason string) error {
	s.log.InfoContext(ctx, "registration refused",
		"slug", slug, "ticket_type_id", ticketTypeID.String(), "reason", reason)
	return apperr.NotFound(apperr.CodeTicketTypeNotFound, registrationRefusal)
}
