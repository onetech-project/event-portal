// Package event owns the events and ticket_types tables: the guest-facing catalog
// reads, the admin CRUD surface, and the atomic quota counter that checkout
// depends on.
package event

import (
	"time"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/pkg/money"
)

// EventSummary is the guest-facing list item (GET /api/v1/events).
//
// BannerURL is the admin-supplied URL string echoed back verbatim — this MVP has
// no object storage and never uploads or serves banner images.
type EventSummary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Venue     string    `json:"venue"`
	Address   string    `json:"address"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	BannerURL *string   `json:"banner_url"`
}

// TicketTypeSummary is a purchasable option on the guest event detail page.
//
// QuotaRemaining maps 1:1 onto ticket_types.quota, which already stores the
// remaining count — no arithmetic against a total is performed, because SCHEMA.md
// stores no total (constitution, Critical Data Flow Rules). 0 means sold out.
type TicketTypeSummary struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Description is the admin-authored note the booking card shows in place of
	// the standard non-refundable wording. Nil or blank falls back to it.
	Description    *string     `json:"description"`
	Price          money.Money `json:"price"`
	QuotaRemaining int32       `json:"quota_remaining"`
	SalesStart     time.Time   `json:"sales_start"`
	SalesEnd       time.Time   `json:"sales_end"`
	// EventStart/EventEnd say when a ticket of this type ADMITS its holder, as
	// opposed to when it may be bought (spec 015). Distinct per ticket type: a
	// multi-day event's Day 1 and Day 2 passes carry different windows even
	// though they share a parent event.
	EventStart time.Time `json:"event_start"`
	EventEnd   time.Time `json:"event_end"`
}

// EventDetail is the guest-facing detail response (GET /api/v1/event/:id).
//
// Content only (clarification 2026-08-05): ticket and package data live behind
// their own endpoints (GET /ticket/:event_id, GET /packages/:event_id) and are
// fetched only when the guest proceeds to the ticket selection page.
type EventDetail struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description *string   `json:"description"`
	Venue       string    `json:"venue"`
	Address     string    `json:"address"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	BannerURL   *string   `json:"banner_url"`
	// Scale is the expected visitor count shown on the info bar as
	// "{count}+ Visitors" (Figma 4-5). Nil hides the cell.
	Scale *int64 `json:"scale"`
	// CMS content blocks (spec 008 US4), position-ordered. Always arrays,
	// never null, so the page can map without guards.
	Activities []ActivityDTO  `json:"activities"`
	GuestStars []GuestStarDTO `json:"guest_stars"`
	Guidelines []GuidelineDTO `json:"guidelines"`
	// HasTerms false disables Buy Ticket with a notice — booking is refused
	// while an event has no authored Terms & Conditions (409001).
	HasTerms bool `json:"has_terms"`
}

// EventTermsDTO is the current Terms & Conditions document shown by the
// booking dialog (GET /ticket/terms-condition/:event_id). Content is sanitized
// HTML authored in the admin CMS; ID is echoed back when agreement is recorded
// so the server can detect the document changing mid-flow (409002).
type EventTermsDTO struct {
	ID        uuid.UUID  `json:"id"`
	Content   string     `json:"content"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// PackageComponentDTO describes one constituent to the client, so the booking
// list can explain what a bundle contains.
type PackageComponentDTO struct {
	TicketTypeID    string `json:"ticket_type_id"`
	TicketTypeName  string `json:"ticket_type_name"`
	QuantityPerUnit int32  `json:"quantity_per_unit"`
}

// PackageSummaryDTO is a bundle row in the public booking list. Money is a
// decimal string; there is deliberately no quota field because a package has
// none (FR-036).
//
// AvailableUnits is derived per request against live quota and must never be
// cached (FR-008); Purchasable already folds in every gate the guest faces.
type PackageSummaryDTO struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Description    *string               `json:"description"`
	Price          money.Money           `json:"price"`
	SalesStart     time.Time             `json:"sales_start"`
	SalesEnd       time.Time             `json:"sales_end"`
	AvailableUnits int32                 `json:"available_units"`
	Purchasable    bool                  `json:"purchasable"`
	Components     []PackageComponentDTO `json:"components"`
}
