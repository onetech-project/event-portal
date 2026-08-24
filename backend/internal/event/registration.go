package event

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RegistrationTarget is everything the free-registration path needs to know
// about the ticket type it is about to issue from (spec 022).
//
// Resolved in ONE call rather than composed by the caller, because deciding
// whether a ticket type belongs to a guest-visible event is this domain's
// business, not the order domain's — and because the registration endpoint must
// answer every refusal identically (FR-012), which is far easier to guarantee
// when one function owns the whole lookup.
type RegistrationTarget struct {
	TicketTypeID   uuid.UUID
	TicketTypeName string
	EventID        uuid.UUID
	EventName      string
	EventSlug      string
	// IsVisible is returned rather than enforced here. The caller
	// refuses, so that a purchasable type and a missing one produce the SAME
	// refusal — this domain must not decide how indistinguishable they look.
	IsVisible      bool
	SalesStart     time.Time
	SalesEnd       time.Time
	QuotaRemaining int32
}

// RegistrationTargetBySlug resolves a ticket type within a guest-visible event.
//
// Returns ErrNotFound when the event is not published, the ticket type does not
// exist, or the two do not belong together. The caller collapses all three onto
// one refusal (FR-011/FR-012), so nothing here may leak which one occurred.
func (s *Service) RegistrationTargetBySlug(ctx context.Context, slug string, ticketTypeID uuid.UUID) (RegistrationTarget, error) {
	ev, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if err != nil {
		return RegistrationTarget{}, err
	}

	row, err := s.repo.GetTicketTypeByID(ctx, nil, ticketTypeID)
	if err != nil {
		return RegistrationTarget{}, err
	}

	// A ticket type id from ANOTHER event is not a different error from a
	// missing one. Both are "no such registration here".
	if row.EventID != ev.ID {
		return RegistrationTarget{}, ErrNotFound
	}

	return RegistrationTarget{
		TicketTypeID:   row.ID,
		TicketTypeName: row.Name,
		EventID:        ev.ID,
		EventName:      ev.Name,
		EventSlug:      ev.Slug,
		IsVisible:      row.IsVisible,
		SalesStart:     row.SalesStart,
		SalesEnd:       row.SalesEnd,
		QuotaRemaining: row.Quota,
	}, nil
}
