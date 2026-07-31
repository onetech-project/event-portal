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
	ID             uuid.UUID   `json:"id"`
	Name           string      `json:"name"`
	Price          money.Money `json:"price"`
	QuotaRemaining int32       `json:"quota_remaining"`
	SalesStart     time.Time   `json:"sales_start"`
	SalesEnd       time.Time   `json:"sales_end"`
}

// EventDetail is the guest-facing detail response (GET /api/v1/events/:slug).
type EventDetail struct {
	ID          uuid.UUID           `json:"id"`
	Name        string              `json:"name"`
	Slug        string              `json:"slug"`
	Description *string             `json:"description"`
	Venue       string              `json:"venue"`
	Address     string              `json:"address"`
	StartDate   time.Time           `json:"start_date"`
	EndDate     time.Time           `json:"end_date"`
	BannerURL   *string             `json:"banner_url"`
	TicketTypes []TicketTypeSummary `json:"ticket_types"`
}
