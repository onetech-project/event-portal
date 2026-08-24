# This PostgreSQL schema must be used as the absolute truth for `sqlc` model generation.

```sql
-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. ADMINS
CREATE TABLE admins (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. EVENTS
CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    venue VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    start_date TIMESTAMP WITH TIME ZONE NOT NULL,
    end_date TIMESTAMP WITH TIME ZONE NOT NULL,
    banner_url TEXT,
    status VARCHAR(50) NOT NULL CHECK (status IN ('DRAFT', 'PUBLISHED', 'COMPLETED')),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    -- Expected visitor count for the detail page's info bar ("30.000+
    -- Visitors"); NULL hides the cell. Added by migration 000006 (spec 008).
    scale BIGINT
);

-- 3. TICKET TYPES
CREATE TABLE ticket_types (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    name VARCHAR(100) NOT NULL,
    -- Admin-authored note rendered on the booking card. Blank falls back to the
    -- standard non-refundable wording.
    description TEXT,
    price NUMERIC(12, 2) NOT NULL,
    quota INT NOT NULL CHECK (quota >= 0), -- Atomic constraint to prevent overselling
    -- When this type may be BOUGHT. Prints nowhere guest-facing; it only gates
    -- whether a row on the selection page is choosable.
    sales_start TIMESTAMP WITH TIME ZONE NOT NULL,
    sales_end TIMESTAMP WITH TIME ZONE NOT NULL,
    -- When a ticket of this type ADMITS its holder (migration 0014, spec 015).
    -- event_start is the instant admission opens, NOT showtime: validation admits
    -- no tolerance (FR-014), so a holder arriving one moment earlier is refused.
    -- Both endpoints inclusive. Backfilled from the parent event, so every
    -- pre-existing ticket type kept displaying exactly what it displayed before.
    -- Deliberately unconstrained against the sales window in either direction
    -- (FR-003) — a ticket may stay on sale after the day it admits to.
    event_start TIMESTAMP WITH TIME ZONE NOT NULL,
    event_end TIMESTAMP WITH TIME ZONE NOT NULL,
    -- Acquisition CHANNEL, not merely listing (migration 0016, spec 022).
    -- FALSE means this type is obtained by REGISTERING at
    -- /events/:slug/register/:id rather than by buying, and carries all three
    -- behaviours: hidden from every guest purchase surface, refused by booking,
    -- checkout and availability, and ineligible for package composition. The
    -- name is narrower than the rule (FR-001a) — there is no way to only hide a
    -- type; clearing this also makes it free to register for.
    -- DEFAULT TRUE, and that is load-bearing: this column is the negation of the
    -- `is_registration_only` it replaced, so a DEFAULT FALSE copied from that
    -- draft would make every ticket type in the system invisible at once, with
    -- no error raised anywhere (SC-009 exists to catch exactly that).
    -- Deliberately unconstrained against `price` (FR-003) — a registration
    -- charges nothing whatever is stored, so pricing these at 0 is convention,
    -- not an invariant, and a CHECK would encode a rule the spec rejects.
    is_visible BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    -- `>=`, unlike packages_sales_window_chk's strict `>`: an event whose
    -- start_date equals its end_date is legal, and the 0014 backfill copies
    -- those dates verbatim, so a strict check would have failed on real rows.
    CONSTRAINT ticket_types_event_window_chk CHECK (event_end >= event_start)
);

-- 4a. ORDER STATUSES (master data, migration 0009; re-keyed by 0013)
-- The status set is data, not a CHECK constraint. orders references a row by id
-- (migration 0013) while the NAME stays the value every interface exchanges and
-- displays — reads alias `order_statuses.name AS status`, writes resolve the
-- name in-statement, so no Go comparison or wire shape changed (spec 011
-- FR-026).
-- The seeded ids are FIXED and part of the schema contract: idx_orders_payment_expiry
-- (bottom of this file) is a partial index, and PostgreSQL requires an immutable
-- predicate — a subquery resolving 'PENDING' is rejected, so the literal 1 is
-- the only option.
CREATE TABLE order_statuses (
    id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY, -- seeded 1=PENDING, 2=PAID, 3=CANCELLED, 4=EXPIRED
    name VARCHAR(256) NOT NULL UNIQUE, -- mandatory + unique: the name is what resolves to this row
    is_active BOOLEAN NOT NULL DEFAULT TRUE, -- retire a status by clearing this, never by deleting the row
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    created_by VARCHAR(50) NOT NULL, -- 'SYSTEM' for the seeded rows; an admin's id once master-list CRUD exists
    updated_at TIMESTAMP WITH TIME ZONE,
    updated_by VARCHAR(50)
);

-- 4b. GENDERS (master data, migration 0008; re-keyed by 0013)
-- The registration forms' gender options (GET /ticket/genders); `name` is the
-- canonical value stored on the wire, and checkout validates submissions
-- against the active rows. attendees.gender_id references the id; the order
-- service maps name <-> id (spec 011 FR-018, FR-025).
CREATE TABLE genders (
    id SMALLINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, -- seeded: 1, 2
    name VARCHAR(100) NOT NULL UNIQUE, -- seeded: FEMALE, MALE
    is_active BOOLEAN NOT NULL DEFAULT TRUE, -- deactivating hides the option; stored references keep resolving
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    created_by VARCHAR(50) NOT NULL, -- 'SYSTEM' for the seeded rows
    updated_at TIMESTAMP WITH TIME ZONE,
    updated_by VARCHAR(50)
);

-- 4c. FEES (master data, migration 0010)
-- What booking applies to the order subtotal: PERCENT takes value% of the
-- subtotal, FIXED is a flat rupiah amount. Admin-managed at /admin/fees;
-- edits affect only future bookings.
CREATE TABLE fees (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE, -- seeded: PPN (11%), Admin Fee (1200)
    fee_type VARCHAR(10) NOT NULL CHECK (fee_type IN ('PERCENT', 'FIXED')),
    value NUMERIC(12, 2) NOT NULL CHECK (value >= 0),
    position INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
);

-- 4d. ORDER FEES (per-order snapshot, migration 0010)
-- The computed fee lines frozen onto the order at booking — name carries any
-- percentage baked in ('PPN (11%)'). Deliberately NOT a foreign key onto the
-- editable master row: history must never change under a fee edit.
CREATE TABLE order_fees (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    amount NUMERIC(12, 2) NOT NULL,
    position INT NOT NULL DEFAULT 0
);

-- 4. ORDERS
-- Two-phase booking (spec 008): booking creates the row with contact fields NULL
-- and payment_expires_at = now()+BOOKING_HOLD; checkout fills the primary
-- contact, calls the gateway, and overwrites payment_expires_at with
-- now()+PAYMENT_WINDOW. The same column carries both deadlines, so one sweeper
-- query expires both phases.
-- total_amount = subtotal + SUM(order_fees.amount); subtotal is NULL on orders
-- that predate fees (migration 0010).
-- Spec 011: there is no buyer form — buyer_* is the primary contact, a snapshot
-- of the TOPMOST holder form (first slot group in canonical slot order). The
-- holder's dob/gender live only on their attendee row (buyer_dob/buyer_gender
-- dropped by migration 0012).
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_number VARCHAR(100) UNIQUE NOT NULL,
    buyer_name VARCHAR(255), -- primary contact — snapshot of the topmost holder form (spec 011); NULL until checkout
    buyer_email VARCHAR(255), -- primary contact email; also the delivery fallback for empty holder emails
    buyer_phone VARCHAR(50), -- primary contact phone; fed to the payment gateway customer details
    subtotal NUMERIC(12, 2), -- pre-fee sum of the lines (migration 0010)
    total_amount NUMERIC(12, 2) NOT NULL,
    status_id INTEGER NOT NULL REFERENCES order_statuses(id), -- was a varchar name FK (0009), re-keyed by 0013; reads alias order_statuses.name AS status, so the wire still carries PENDING/PAID/...
    payment_provider VARCHAR(50), 
    payment_url TEXT, -- provider's generate-qr-code action URL (audit/fallback); not a page the guest is sent to
    payment_qr_string TEXT, -- raw QRIS payload; the QR image is rendered from this on demand, never stored
    payment_expires_at TIMESTAMP WITH TIME ZONE, -- server-owned deadline: 1h booking hold, then 14m payment window
    terms_agreed_at TIMESTAMP WITH TIME ZONE, -- durable T&C agreement record; checkout refuses NULL
    event_terms_id UUID REFERENCES event_terms(id) ON DELETE SET NULL, -- which terms document was agreed
    email_sent BOOLEAN DEFAULT FALSE,
    -- NOTE: there is deliberately NO is_registration column (spec 022, decision
    -- reversed 2026-08-20; Constitution Principle IV v6.0.0).
    --
    -- PAID has two origins — a gateway-settled purchase and a free registration —
    -- and the second is DERIVED, not stored: an order is a registration when it
    -- carries an order_items line for a ticket type with is_visible = FALSE.
    -- Containment guarantees such a type can never be bought or bundled, so one
    -- such line can only have come from the registration path.
    --
    -- Any surface reporting revenue MUST apply that derivation; the status alone
    -- no longer implies money moved.
    --
    -- The accepted cost, recorded because the schema cannot enforce it: the value
    -- is not stable over time. Making an invitation ticket type purchasable again
    -- retroactively reclassifies every historical order that used it.
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 5. ORDER ITEMS
-- A XOR line: either an individual ticket_type line or a package line, never both.
-- A package line is stored ONCE at the package's own price, so total_amount stays
-- exactly SUM(quantity * price) with no invented per-constituent price allocation.
CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    ticket_type_id UUID REFERENCES ticket_types(id) ON DELETE RESTRICT,
    package_id UUID REFERENCES packages(id) ON DELETE RESTRICT,
    quantity INT NOT NULL,
    price NUMERIC(12, 2) NOT NULL,
    CONSTRAINT order_items_line_kind_chk CHECK (
        (ticket_type_id IS NOT NULL AND package_id IS NULL) OR
        (ticket_type_id IS NULL     AND package_id IS NOT NULL)
    ),
    CONSTRAINT order_items_quantity_chk CHECK (quantity > 0)
);

-- 6. ATTENDEES (Dynamic forms output)
-- ticket_type_id stays NOT NULL even for bundle-derived registrants: that is what
-- preserves one-pass-per-attendee for packages. package_id records only which bundle
-- the slot came from.
-- Spec 008: rows are created as EMPTY SLOTS inside the booking transaction
-- (identity columns NULL) and filled at checkout; the order service enforces
-- completeness before payment starts, so PAID orders always have full slots.
-- Spec 010: package_unit is the ordinal of the purchased bundle unit the slot
-- belongs to (1..quantity of its package order line) — one visitor form fills a
-- whole unit, and checkout requires identical visitor data within it. NULL for
-- standalone slots and for bundle slots booked before spec 010 (those keep the
-- pre-010 one-form-per-slot behavior).
-- Spec 011: gender is a real reference to the genders master (migration 0012
-- replaced the free-text column + fixed CHECK with gender_id). The API keeps
-- exchanging the gender ID (migration 0017, 2026-08-24): a form submits
-- gender_id, and the order readback carries both gender_id and the display name
-- — a retired entry is absent from the active master list, so a client given
-- only one could resolve neither the other nor the restored-form case.
-- Phone is validated digits-only 10-12 at checkout since spec 011 (no CHECK —
-- pre-011 rows keep their 7-20-digit-era values).
CREATE TABLE attendees (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    ticket_type_id UUID NOT NULL REFERENCES ticket_types(id) ON DELETE RESTRICT,
    package_id UUID REFERENCES packages(id) ON DELETE RESTRICT,
    package_unit SMALLINT CHECK (package_unit IS NULL OR package_unit >= 1),
    name VARCHAR(255), -- NULL while the slot is unfilled (pre-checkout)
    email VARCHAR(255), -- per-holder e-ticket delivery address (spec 011)
    phone VARCHAR(50),
    dob DATE,
    gender_id SMALLINT REFERENCES genders(id) -- NULL while unfilled; name<->id mapped in the order service (0012, re-keyed by 0013)
);

-- 7. TICKETS (Generated after PAID)
CREATE TABLE tickets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ticket_code VARCHAR(100) UNIQUE NOT NULL,
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    attendee_id UUID NOT NULL REFERENCES attendees(id) ON DELETE CASCADE,
    status VARCHAR(50) NOT NULL CHECK (status IN ('ACTIVE', 'USED', 'REVOKED')),
    qr_code_url TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 8. PAYMENTS (Webhook & Transaction Logs)
-- Append-only. Rows are never updated in place: the sequence IS the audit trail.
-- `status` holds either the provider's RAW transaction status or one of this
-- domain's own markers (DISPUTED, SETTLED_AFTER_EXPIRY, SETTLE_REFUSED_NO_QUOTA,
-- SESSION_DUPLICATE, SESSION_OPENED) — both live in one column, which is what
-- makes the sequence a single readable narrative.
CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,
    transaction_id VARCHAR(255) NOT NULL, -- the gateway's network transaction id; falls back to the order number on a marker row that has none of its own
    payment_type VARCHAR(100),
    status VARCHAR(50) NOT NULL,
    raw_response JSONB,
    ext_ref_id VARCHAR(255), -- gateway's own reference (eri) for the payment session, captured at session open (migration 0015)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 9. PACKAGES (Bundle offers)
-- DELIBERATELY has no quota / inventory / stock column. Availability is derived at
-- read time from the remaining quota of the ticket_types reached through
-- package_tickets. An inventory column here would be a second source of truth.
CREATE TABLE packages (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price NUMERIC(12, 2) NOT NULL CHECK (price >= 0),
    sales_start TIMESTAMP WITH TIME ZONE NOT NULL,
    sales_end TIMESTAMP WITH TIME ZONE NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE, -- was status ACTIVE|INACTIVE (migration 0013); a boolean on the wire too, unlike the order status
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT packages_sales_window_chk CHECK (sales_end > sales_start),
    CONSTRAINT packages_id_event_uk UNIQUE (id, event_id)
);

-- 10. PACKAGE_TICKETS (Composition junction)
-- event_id is denormalised so the two composite foreign keys make cross-event
-- composition structurally impossible: both resolve against the same event_id value.
-- Requires ticket_types_id_event_uk UNIQUE (id, event_id) on ticket_types.
CREATE TABLE package_tickets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    package_id UUID NOT NULL,
    ticket_type_id UUID NOT NULL,
    event_id UUID NOT NULL,
    quantity INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT package_tickets_package_fk FOREIGN KEY (package_id, event_id)
        REFERENCES packages (id, event_id) ON DELETE CASCADE,
    CONSTRAINT package_tickets_ticket_type_fk FOREIGN KEY (ticket_type_id, event_id)
        REFERENCES ticket_types (id, event_id) ON DELETE RESTRICT,
    CONSTRAINT package_tickets_unique_member UNIQUE (package_id, ticket_type_id)
);

-- 11. EVENT_TERMS (spec 008 — one live T&C document per event)
-- content is sanitized HTML: the API sanitizes on write (bluemonday), so read
-- paths render it verbatim. CASCADE: content must not block event deletion.
CREATE TABLE event_terms (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 12. EVENT_ACTIVITIES (spec 008 — detail-page content blocks)
-- icon is a named key from a fixed client-side set, never a URL (no object storage).
CREATE TABLE event_activities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    icon VARCHAR(100),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 13. EVENT_GUEST_STARS (spec 008 — lineup names)
CREATE TABLE event_guest_stars (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 14. EVENT_GUIDELINES (spec 008 — visitor rules on the detail page)
CREATE TABLE event_guidelines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    description VARCHAR(500) NOT NULL,
    icon VARCHAR(100),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- INDEXES FOR PERFORMANCE
CREATE INDEX idx_events_slug ON events(slug);
CREATE INDEX idx_orders_order_number ON orders(order_number);
CREATE INDEX idx_tickets_ticket_code ON tickets(ticket_code);
CREATE INDEX idx_ticket_types_event_id ON ticket_types(event_id);
CREATE INDEX idx_packages_event_id ON packages(event_id);
CREATE INDEX idx_package_tickets_package_id ON package_tickets(package_id);
CREATE INDEX idx_package_tickets_ticket_type_id ON package_tickets(ticket_type_id);
-- Partial: package lines are the minority and the only rows the delete-guard scans.
CREATE INDEX idx_order_items_package_id ON order_items(package_id) WHERE package_id IS NOT NULL;
CREATE INDEX idx_attendees_order_id ON attendees(order_id);
-- attendees.email is deliberately UNINDEXED. A draft of spec 022 added
-- idx_attendees_email_lower for a per-address duplicate check; FR-023 removed
-- that rule (an address may register as many times as quota allows), and no
-- other read filters on the address. An index kept "in case" would be pure write
-- amplification on attendee insert, which is the path this feature loads.

-- The expiry sweeper's only query: PENDING orders whose payment deadline passed.
-- Partial, so it stays roughly the size of the live payment window.
-- Partial on the LITERAL seeded id, not a name lookup: a partial index predicate
-- must be immutable, so PostgreSQL rejects a subquery here. This is why
-- order_statuses.id = 1 is PENDING by contract (migration 0013).
CREATE INDEX idx_orders_payment_expiry ON orders (payment_expires_at) WHERE status_id = 1;
-- Spec 008 content lookups, all by owning event.
CREATE INDEX idx_event_terms_event_id ON event_terms(event_id);
CREATE INDEX idx_event_activities_event_id ON event_activities(event_id);
CREATE INDEX idx_event_guest_stars_event_id ON event_guest_stars(event_id);
CREATE INDEX idx_event_guidelines_event_id ON event_guidelines(event_id);