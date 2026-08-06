# Contract: Models — Go structs & TypeScript interfaces

Companion to `../spec.md`. Types follow the conventions already in the repo:

- **Go persistence structs** match what `sqlc` will generate from
  `backend/sqlc.yaml` — `uuid.UUID`, `uuid.NullUUID` for nullable UUIDs,
  `decimal.Decimal` for `NUMERIC`, `time.Time` (`*time.Time` when nullable). They are
  shown here so the hand-written domain types and DTOs around them can be reviewed;
  the real files are generated, never hand-edited.
- **Go DTOs** are hand-written per Constitution Principle III — `sqlc` structs never
  reach an HTTP response.
- **TypeScript** mirrors `frontend/lib/types.ts`: `snake_case` wire fields, money as
  decimal **strings** so no precision is lost in JSON.

---

## 1. Go — persistence layer (sqlc-generated shape)

Generated into `internal/event/eventsql` (packages are owned by the `event` domain: they
belong to an event and are composed of that event's ticket types, so no domain boundary is
crossed).

```go
// Package is a bundle offer. Note what is NOT here: no Quota, no Stock, no Remaining.
// A package's availability is derived from its constituents at read time; giving this
// struct an inventory field would create a second source of truth.
type Package struct {
	ID          uuid.UUID
	EventID     uuid.UUID
	Name        string
	Description *string
	Price       decimal.Decimal
	SalesStart  time.Time
	SalesEnd    time.Time
	Status      string // 'ACTIVE' | 'INACTIVE'
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

// PackageTicket is one line of a package's composition: how many units of a given
// ticket type one unit of the package consumes.
type PackageTicket struct {
	ID           uuid.UUID
	PackageID    uuid.UUID
	TicketTypeID uuid.UUID
	EventID      uuid.UUID // denormalised; drives the same-event composite FKs
	Quantity     int32
	CreatedAt    *time.Time
}
```

Changed generated structs in `internal/order/ordersql`:

```go
type OrderItem struct {
	ID    uuid.UUID
	OrderID uuid.UUID
	// Exactly one of TicketTypeID / PackageID is valid — enforced by
	// order_items_line_kind_chk. A package line is stored once at the package's own
	// price, never decomposed into per-constituent price allocations.
	TicketTypeID uuid.NullUUID
	PackageID    uuid.NullUUID
	Quantity     int32
	Price        decimal.Decimal
}

type Attendee struct {
	ID      uuid.UUID
	OrderID uuid.UUID
	// Always set, including for bundle-derived registrants. This is what keeps
	// one-pass-per-attendee intact for bundles.
	TicketTypeID uuid.UUID
	// Set only when this registrant slot came from a package.
	PackageID uuid.NullUUID
	Name      string
	Email     string
}
```

## 2. Go — domain types

### 2.1 Availability

```go
// PackageAvailability is the derived, never-stored inventory view of a package.
type PackageAvailability struct {
	PackageID uuid.UUID
	// AvailableUnits is min over constituents of floor(quota / quantity): the number
	// of COMPLETE package sets currently coverable. Whole sets only — a constituent
	// with 5 remaining consumed 2 at a time yields 2, not 2.5.
	AvailableUnits int32
	// Purchasable folds in every gate the guest actually faces: units > 0, the
	// package's own sales window, the event's published state, and every
	// constituent's sales window.
	Purchasable bool
	// LimitingTicketTypeID names the scarcest constituent — the one that will be
	// reported when a checkout is rejected.
	LimitingTicketTypeID uuid.UUID
}
```

### 2.2 Composition, as checkout needs it

```go
// PackageComponent is one constituent of a package, resolved for checkout.
type PackageComponent struct {
	TicketTypeID uuid.UUID
	Name         string
	// PerUnit is how many of this ticket type ONE package unit consumes.
	PerUnit    int32
	SalesStart time.Time
	SalesEnd   time.Time
}

// PackageForCheckout is the server-side truth about a package at checkout time:
// authoritative price, sales window, and full composition. Client-supplied prices are
// ignored in favour of this.
type PackageForCheckout struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	SalesStart time.Time
	SalesEnd   time.Time
	Status     string
	Components []PackageComponent
}
```

### 2.3 Extension to the existing `EventProvider` port

The `order` domain already declares what it needs from `event` as a consumer-side interface
(`backend/internal/order/event_provider.go`). Packages extend it rather than introducing a
second port, so `order` still imports nothing from `internal/event`.

```go
type EventProvider interface {
	// --- existing, unchanged ---
	TicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (TicketTypeInfo, error)
	CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
	RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error

	// --- added for packages ---

	// PackageForCheckout returns the server's authoritative price, sales window and
	// full composition for a package. Takes the caller's transaction for the same
	// reason the ticket-type variant does: borrowing a second connection while
	// holding one can exhaust the pool and deadlock under concurrency.
	PackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PackageForCheckout, error)
}
```

Deliberately **not** added: a `CheckAndDeductPackageQuota`. A package has no quota to deduct.
Checkout expands packages into per-ticket-type demand and reuses the existing
`CheckAndDeductQuota` — one code path guards inventory, which is what makes the
single-source-of-truth rule hold rather than merely being stated.

### 2.4 Checkout request shapes

```go
// CheckoutItem is one selected row. Exactly one of TicketTypeID / PackageID is set,
// mirroring the XOR on order_items.
type CheckoutItem struct {
	TicketTypeID *uuid.UUID `json:"ticket_type_id,omitempty"`
	PackageID    *uuid.UUID `json:"package_id,omitempty"`
	Quantity     int32      `json:"quantity"`
}

// CheckoutAttendee is one registrant slot. For a bundle-derived slot both PackageID and
// TicketTypeID are set: the guest fills one form per constituent unit, and those forms
// may name different people.
type CheckoutAttendee struct {
	TicketTypeID uuid.UUID  `json:"ticket_type_id"`
	PackageID    *uuid.UUID `json:"package_id,omitempty"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
}
```

### 2.5 Response DTOs

```go
// PackageComponentDTO describes one constituent to the client, so the booking list can
// explain what a bundle contains.
type PackageComponentDTO struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	QuantityPerUnit int32 `json:"quantity_per_unit"`
}

// PackageSummaryDTO is a bundle row in the public booking list. Money is a decimal
// string; there is no quota field because a package has none.
type PackageSummaryDTO struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description *string               `json:"description"`
	Price       string                `json:"price"`
	SalesStart  string                `json:"sales_start"`
	SalesEnd    string                `json:"sales_end"`
	// AvailableUnits is derived per request and never cached.
	AvailableUnits int32               `json:"available_units"`
	Purchasable    bool                `json:"purchasable"`
	Components     []PackageComponentDTO `json:"components"`
}

// PackageAdminDTO is the administrator's view. It too has no quota field — the admin
// surface must never present or accept one (FR-036).
type PackageAdminDTO struct {
	ID          string                `json:"id"`
	EventID     string                `json:"event_id"`
	Name        string                `json:"name"`
	Description *string               `json:"description"`
	Price       string                `json:"price"`
	SalesStart  string                `json:"sales_start"`
	SalesEnd    string                `json:"sales_end"`
	Status      string                `json:"status"`
	Components  []PackageComponentDTO `json:"components"`
	// Derived, read-only.
	AvailableUnits int32  `json:"available_units"`
	Sold           int32  `json:"sold"`
	CreatedAt      *string `json:"created_at"`
	UpdatedAt      *string `json:"updated_at"`
}
```

---

## 3. TypeScript — wire types

Append to `frontend/lib/types.ts`.

```ts
export type PackageComponent = {
  ticket_type_id: string;
  ticket_type_name: string;
  /** How many of this ticket one bundle unit consumes. */
  quantity_per_unit: number;
};

/**
 * A bundle row in the booking list. It has no quota of its own by design —
 * `available_units` is derived server-side from the remaining quota of the tickets
 * inside it, freshly per request. Never cache it.
 */
export type PackageSummary = {
  id: string;
  name: string;
  description: string | null;
  price: string;
  sales_start: string;
  sales_end: string;
  /** Complete sets still coverable. 0 means the bundle is sold out. */
  available_units: number;
  /** Folds in available_units, the bundle's window, and every constituent's window. */
  purchasable: boolean;
  components: PackageComponent[];
};

/** EventDetail gains a sibling list to ticket_types; the booking list renders both. */
export type EventDetailWithPackages = EventDetail & {
  packages: PackageSummary[];
};

/** One row of the booking list, discriminated so the UI can badge bundles. */
export type SelectableItem =
  | { kind: "ticket"; ticket: TicketTypeSummary }
  | { kind: "package"; pkg: PackageSummary };

/** One line of the running selection summary. */
export type SelectionLine = {
  kind: "ticket" | "package";
  id: string;
  name: string;
  unit_price: string;
  quantity: number;
  /** Upper bound for the stepper: quota_remaining, or available_units for a bundle. */
  max_quantity: number;
};

/** Exactly one of ticket_type_id / package_id is set, mirroring the server's XOR. */
export type CheckoutItemInput =
  | { ticket_type_id: string; quantity: number }
  | { package_id: string; quantity: number };

/**
 * One registrant slot. A bundle-derived slot carries both ids: the ticket it grants
 * access to, and the bundle it came from. The two slots of one bundle unit may name
 * different people.
 */
export type CheckoutAttendeeInput = {
  ticket_type_id: string;
  package_id?: string;
  name: string;
  email: string;
};

export type CheckoutRequestV2 = {
  buyer_name: string;
  buyer_email: string;
  buyer_phone: string;
  items: CheckoutItemInput[];
  attendees: CheckoutAttendeeInput[];
};

/** Admin view. No quota field, deliberately — FR-036. */
export type PackageAdminView = {
  id: string;
  event_id: string;
  name: string;
  description: string | null;
  price: string;
  sales_start: string;
  sales_end: string;
  status: "ACTIVE" | "INACTIVE";
  components: PackageComponent[];
  /** Derived, read-only. */
  available_units: number;
  /** Derived from order lines, never stored. */
  sold: number;
  created_at: string | null;
  updated_at: string | null;
};

export type PackageUpsertInput = {
  name: string;
  description: string | null;
  price: string;
  sales_start: string;
  sales_end: string;
  status: "ACTIVE" | "INACTIVE";
  /** At least one entry; a ticket may appear at most once. */
  components: { ticket_type_id: string; quantity_per_unit: number }[];
};
```

`PublicOrderItem` gains a discriminator so an order containing a bundle reads correctly:

```ts
export type PublicOrderItem = {
  /** "package" lines show the bundle's name and its own all-in price. */
  kind: "ticket" | "package";
  ticket_type_name: string | null;
  package_name: string | null;
  quantity: number;
  unit_price: string;
  subtotal: string;
};
```

## 4. Derived selection maths (frontend)

Read-only helpers; the server remains the authority on both price and availability.

```ts
/** Stepper ceiling for a row. */
export function maxQuantityFor(item: SelectableItem): number {
  return item.kind === "ticket"
    ? item.ticket.quota_remaining
    : item.pkg.available_units;
}

/**
 * How many registrant forms a selection needs, per (ticket, origin bundle) slot.
 * A bundle of Day1 + Day2 at quantity 1 produces two slots.
 */
export function attendeeSlotCount(lines: SelectionLine[], pkgs: PackageSummary[]): number {
  return lines.reduce((n, line) => {
    if (line.kind === "ticket") return n + line.quantity;
    const pkg = pkgs.find((p) => p.id === line.id);
    if (!pkg) return n;
    const perUnit = pkg.components.reduce((s, c) => s + c.quantity_per_unit, 0);
    return n + line.quantity * perUnit;
  }, 0);
}
```

Money is never summed client-side for anything the guest is charged: the displayed total is
presentational, and `POST /checkout` recomputes it from stored prices (FR-025).
