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
    sales_start TIMESTAMP WITH TIME ZONE NOT NULL,
    sales_end TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 4a. ORDER STATUSES (master data, migration 0009)
-- The status set is data, not a CHECK constraint: orders.status keeps its
-- varchar value (status-filtering queries untouched) but is foreign-keyed here.
CREATE TABLE order_statuses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL UNIQUE, -- seeded: PENDING, PAID, CANCELLED, EXPIRED
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
);

-- 4b. GENDERS (master data, migration 0008)
-- The registration forms' gender options (GET /ticket/genders); `name` is the
-- canonical value stored on orders.buyer_gender and attendees.gender, and
-- checkout validates submissions against the active rows.
CREATE TABLE genders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(50) NOT NULL UNIQUE, -- seeded: FEMALE, MALE
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE
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
-- Two-phase booking (spec 008): booking creates the row with buyer fields NULL
-- and payment_expires_at = now()+BOOKING_HOLD; checkout fills the buyer, calls
-- the gateway, and overwrites payment_expires_at with now()+PAYMENT_WINDOW. The
-- same column carries both deadlines, so one sweeper query expires both phases.
-- total_amount = subtotal + SUM(order_fees.amount); subtotal is NULL on orders
-- that predate fees (migration 0010).
CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_number VARCHAR(100) UNIQUE NOT NULL,
    buyer_name VARCHAR(255), -- NULL until checkout (booking precedes buyer entry)
    buyer_email VARCHAR(255),
    buyer_phone VARCHAR(50),
    buyer_dob DATE, -- buyer form mirrors a visitor card (Figma 12-4456, migration 0007)
    buyer_gender VARCHAR(10),
    subtotal NUMERIC(12, 2), -- pre-fee sum of the lines (migration 0010)
    total_amount NUMERIC(12, 2) NOT NULL,
    status VARCHAR(50) NOT NULL REFERENCES order_statuses(name), -- CHECK replaced by FK (migration 0009)
    payment_provider VARCHAR(50), 
    payment_url TEXT, -- provider's generate-qr-code action URL (audit/fallback); not a page the guest is sent to
    payment_qr_string TEXT, -- raw QRIS payload; the QR image is rendered from this on demand, never stored
    payment_expires_at TIMESTAMP WITH TIME ZONE, -- server-owned deadline: 1h booking hold, then 14m payment window
    terms_agreed_at TIMESTAMP WITH TIME ZONE, -- durable T&C agreement record; checkout refuses NULL
    event_terms_id UUID REFERENCES event_terms(id) ON DELETE SET NULL, -- which terms document was agreed
    email_sent BOOLEAN DEFAULT FALSE,
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
CREATE TABLE attendees (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    ticket_type_id UUID NOT NULL REFERENCES ticket_types(id) ON DELETE RESTRICT,
    package_id UUID REFERENCES packages(id) ON DELETE RESTRICT,
    package_unit SMALLINT CHECK (package_unit IS NULL OR package_unit >= 1),
    name VARCHAR(255), -- NULL while the slot is unfilled (pre-checkout)
    email VARCHAR(255),
    phone VARCHAR(50),
    dob DATE,
    gender VARCHAR(20) CHECK (gender IN ('MALE', 'FEMALE'))
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
CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    provider VARCHAR(50) NOT NULL,
    transaction_id VARCHAR(255) NOT NULL,
    payment_type VARCHAR(100),
    status VARCHAR(50) NOT NULL,
    raw_response JSONB,
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
    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
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

-- The expiry sweeper's only query: PENDING orders whose payment deadline passed.
-- Partial, so it stays roughly the size of the live payment window.
CREATE INDEX idx_orders_payment_expiry ON orders (payment_expires_at) WHERE status = 'PENDING';
-- Spec 008 content lookups, all by owning event.
CREATE INDEX idx_event_terms_event_id ON event_terms(event_id);
CREATE INDEX idx_event_activities_event_id ON event_activities(event_id);
CREATE INDEX idx_event_guest_stars_event_id ON event_guest_stars(event_id);
CREATE INDEX idx_event_guidelines_event_id ON event_guidelines(event_id);