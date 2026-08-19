package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
)

// EvaluateAvailability answers whether a selection can be bought right now
// (spec 013, POST /ticket/availability). It runs in front of the Terms &
// Conditions gate so a guest never reads a document for a purchase that was
// never going to happen.
//
// It is ADVISORY. Nothing is reserved, no order is created, no row is locked,
// and an "available" answer confers no right to book: between this decision and
// the guest's Agree click another buyer can take the last seat. Booking remains
// the sole authority, and every refusal path inside bookOnce stays exactly
// where it is (FR-011).
//
// Two properties matter more than the answer itself:
//
//   - It reuses expandItem, the same function booking uses, so the check refuses
//     exactly what booking would refuse. Before the 2026-08-19 amendment that
//     reuse was defended as wording parity; the guest now reads neither
//     sentence, so what it buys is VERDICT parity — which is what FR-005 and
//     FR-011 actually rest on, and no less load-bearing for the change.
//   - It takes NO row locks. Principle IV's rationale is that the quota
//     -deducting UPDATE holds a lock until commit, so anything inside such a
//     transaction serializes every concurrent buyer of that ticket type. An
//     advisory check that took those locks would inherit that cost while
//     returning an answer that can be stale a millisecond later.
//
// A malformed request is an error, not a decision — a question that cannot be
// asked has no answer.
func (s *Service) EvaluateAvailability(ctx context.Context, req AvailabilityRequest) (AvailabilityDecision, error) {
	if err := req.Validate(); err != nil {
		return AvailabilityDecision{}, err
	}

	reasons := make([]AvailabilityReason, 0)

	// One transaction, purely for a consistent snapshot: a selection spanning
	// five ticket types judged against five different instants could report a
	// picture that was never true at once. It writes nothing.
	txErr := db.InTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		now := s.now()

		expanded, lineReasons := s.expandForCheck(ctx, tx, req, now)
		reasons = append(reasons, lineReasons...)

		// Aggregate before judging quota, so a bundle and a standalone ticket
		// drawing on the same ticket type are weighed together rather than each
		// against the full remaining quota (FR-005). This is the same rule
		// aggregateDemand enforces inside the booking transaction, and skipping
		// it here would approve a selection booking then refuses.
		quotaReasons, err := s.quotaShortfalls(ctx, tx, expanded, now)
		if err != nil {
			return err
		}
		reasons = append(reasons, quotaReasons...)

		return nil
	})
	if txErr != nil {
		return AvailabilityDecision{}, txErr
	}

	// Order-level, and outside the transaction because it needs none: Book runs
	// the same guard as a read-only pre-transaction check. Without it the guest
	// opens a dialog whose body reads "not available yet" and whose Agree button
	// could never usefully be pressed.
	if _, err := s.events.CurrentTerms(ctx, req.EventID); err != nil {
		if !errors.Is(err, ErrNoTerms) {
			return AvailabilityDecision{}, err
		}
		reasons = append(reasons, AvailabilityReason{
			Code:    apperr.CodeTermsMissing,
			Message: "This event has no Terms & Conditions to agree to yet.",
		})
	}

	decision := AvailabilityDecision{Available: len(reasons) == 0, Reasons: reasons}

	// FR-006, as amended 2026-08-19: the per-line detail is no longer shown to
	// the guest, who reads one general sentence however many lines are at fault.
	// It is kept so a refusal can be explained afterwards — which requires a
	// record, and a 200 never reaches the error handler where booking's refusals
	// are logged. Outside the transaction: it holds no locks, but there is no
	// reason to keep one open across a write to the log.
	if !decision.Available {
		s.log.WarnContext(ctx, "availability check refused",
			"event_id", req.EventID,
			"item_count", len(req.Items),
			"reason_codes", reasonCodes(reasons),
		)
	}

	return decision, nil
}

// reasonCodes lists the stable codes of a refusal, in the order they were found.
//
// Codes only: the messages are formatted for a reader and the identifiers are
// already on the reasons themselves, so a log line built from them stays
// greppable and cannot grow to the size of the selection.
func reasonCodes(reasons []AvailabilityReason) []string {
	codes := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		codes = append(codes, reason.Code)
	}
	return codes
}

// expandForCheck resolves every line, collecting failures instead of returning
// on the first one.
//
// This is the single behavioural difference from booking, and it is deliberate:
// bookOnce holds row locks and should abort the moment it knows the answer, while
// this holds none and can afford to evaluate the whole selection. Since the
// 2026-08-19 amendment the guest is told none of it — they read one general
// sentence (FR-012) — so the collection survives as the diagnostic record FR-006
// requires, not as copy.
//
// A line that fails contributes no demand — its quantity is unknowable against a
// ticket type that may not exist.
func (s *Service) expandForCheck(
	ctx context.Context,
	tx pgx.Tx,
	req AvailabilityRequest,
	now time.Time,
) ([]ExpandedItem, []AvailabilityReason) {
	expanded := make([]ExpandedItem, 0, len(req.Items))
	reasons := make([]AvailabilityReason, 0)

	for i, item := range req.Items {
		index := i

		expandedItem, err := expandItem(ctx, s.events, tx, item, now)
		if err != nil {
			reasons = append(reasons, reasonFor(index, item, err))
			continue
		}

		// The same event-scoping rule booking applies: an item from another
		// event must not be bought under this one.
		if expandedItem.EventID != req.EventID {
			reasons = append(reasons, AvailabilityReason{
				ItemIndex:    &index,
				TicketTypeID: item.TicketTypeID,
				PackageID:    item.PackageID,
				Code:         apperr.CodeValidation,
				Message:      "All items must belong to the event being booked.",
			})
			continue
		}

		expanded = append(expanded, expandedItem)
	}

	return expanded, reasons
}

// quotaShortfalls compares aggregated per-ticket-type demand against remaining
// quota, read WITHOUT a lock.
//
// A shortfall is reported against every line that contributed demand to that
// ticket type, not against the ticket type alone: a bundle whose constituent ran
// out has to be recorded as the bundle that was chosen, or the record cannot be
// read back against the selection that produced it.
func (s *Service) quotaShortfalls(
	ctx context.Context,
	tx pgx.Tx,
	expanded []ExpandedItem,
	now time.Time,
) ([]AvailabilityReason, error) {
	demand := aggregateDemand(expanded)
	reasons := make([]AvailabilityReason, 0)

	// Sorted for a stable response order, not for lock ordering — there are no
	// locks here to order.
	for _, ttID := range sortedTicketTypeIDs(demand) {
		info, err := s.events.TicketTypeForCheckout(ctx, tx, ttID)
		if err != nil {
			// Reachable only if a constituent vanished between the package read
			// and this one; the line-level pass already reports anything the
			// guest selected directly.
			var appErr *apperr.Error
			if errors.As(err, &appErr) {
				reasons = append(reasons, AvailabilityReason{
					TicketTypeID: &ttID, Code: appErr.Code, Message: appErr.Message,
				})
				continue
			}
			return nil, err
		}

		wanted := demand[ttID]
		if wanted <= info.QuotaRemaining {
			continue
		}

		// Worded exactly as bookOnce words it. Since the 2026-08-19 amendment this
		// sentence is a record rather than copy: FR-013 forbids it reaching the
		// guest at either point, and the client collapses it to the general
		// message. Kept identical to booking's so the two records match.
		message := fmt.Sprintf("Only fewer than %d ticket(s) remain.", wanted)
		for _, index := range linesDemanding(expanded, ttID) {
			line := index
			reasons = append(reasons, AvailabilityReason{
				ItemIndex:    &line,
				TicketTypeID: &ttID,
				PackageID:    packageIDOf(expanded[index]),
				Code:         apperr.CodeInsufficientQuota,
				Message:      message,
			})
		}
	}

	return reasons, nil
}

// linesDemanding returns the indices into expanded of every line drawing on
// ticketTypeID — one standalone line, every bundle containing it, or both.
func linesDemanding(expanded []ExpandedItem, ticketTypeID uuid.UUID) []int {
	indices := make([]int, 0, 1)
	for i, item := range expanded {
		if _, draws := item.Demand[ticketTypeID]; draws {
			indices = append(indices, i)
		}
	}
	return indices
}

func packageIDOf(item ExpandedItem) *uuid.UUID {
	if !item.Ref.PackageID.Valid {
		return nil
	}
	id := item.Ref.PackageID.UUID
	return &id
}

// reasonFor turns a refusal from expandItem into a line-addressed reason,
// carrying the code and message through unchanged.
func reasonFor(index int, item CheckoutItem, err error) AvailabilityReason {
	reason := AvailabilityReason{
		ItemIndex:    &index,
		TicketTypeID: item.TicketTypeID,
		PackageID:    item.PackageID,
	}

	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		reason.Code = appErr.Code
		reason.Message = appErr.Message
		return reason
	}

	// Not expected: expandItem's failures are all apperr. Reported rather than
	// swallowed so an unclassified fault cannot masquerade as availability.
	reason.Code = apperr.CodeInternal
	reason.Message = "This item could not be checked. Please try again."
	return reason
}
