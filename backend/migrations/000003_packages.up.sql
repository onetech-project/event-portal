-- Ticket package bundles (spec 005).
--
-- A package is an offer composed of this event's ticket types. It deliberately has
-- NO quota column: availability is derived at read time from the remaining quota of
-- its constituents. A stored inventory here would be a second source of truth and
-- would reintroduce the overselling the guarded ticket_types UPDATE prevents.

BEGIN;

-- FK targets for the composite foreign keys on package_tickets below. No-ops for
-- uniqueness (id is already the PK) — they exist only so a composite FK can resolve.
ALTER TABLE ticket_types ADD CONSTRAINT ticket_types_id_event_uk UNIQUE (id, event_id);

-- 9. PACKAGES (bundle offers)
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

-- 10. PACKAGE_TICKETS (composition junction)
-- event_id is denormalised solely so the two composite foreign keys can make
-- cross-event composition structurally impossible rather than merely discouraged: a
-- package on event A can never reference a ticket_type on event B, because both FKs
-- resolve against the same event_id value in this row.
CREATE TABLE package_tickets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    package_id UUID NOT NULL,
    ticket_type_id UUID NOT NULL,
    event_id UUID NOT NULL,
    quantity INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT package_tickets_package_fk FOREIGN KEY (package_id, event_id)
        REFERENCES packages (id, event_id) ON DELETE CASCADE,

    -- RESTRICT: a ticket type inside a bundle cannot be deleted until it is removed
    -- from that bundle.
    CONSTRAINT package_tickets_ticket_type_fk FOREIGN KEY (ticket_type_id, event_id)
        REFERENCES ticket_types (id, event_id) ON DELETE RESTRICT,

    -- A ticket appears at most once per package; repeats use quantity instead.
    CONSTRAINT package_tickets_unique_member UNIQUE (package_id, ticket_type_id)
);

-- order_items becomes a XOR line: either an individual ticket_type line or a package
-- line. A package line is stored ONCE at the package's own price, so total_amount
-- stays exactly SUM(quantity * price) and no per-constituent price split is invented.
ALTER TABLE order_items ADD COLUMN package_id UUID REFERENCES packages(id) ON DELETE RESTRICT;
ALTER TABLE order_items ALTER COLUMN ticket_type_id DROP NOT NULL;
ALTER TABLE order_items ADD CONSTRAINT order_items_line_kind_chk CHECK (
    (ticket_type_id IS NOT NULL AND package_id IS NULL) OR
    (ticket_type_id IS NULL     AND package_id IS NOT NULL)
);
ALTER TABLE order_items ADD CONSTRAINT order_items_quantity_chk CHECK (quantity > 0);

-- attendees keeps ticket_type_id NOT NULL: every registrant is still bound to exactly
-- one ticket type, which is what preserves one-pass-per-attendee for bundles.
-- package_id records only which bundle the slot came from.
ALTER TABLE attendees ADD COLUMN package_id UUID REFERENCES packages(id) ON DELETE RESTRICT;

CREATE INDEX idx_packages_event_id              ON packages(event_id);
CREATE INDEX idx_package_tickets_package_id     ON package_tickets(package_id);
CREATE INDEX idx_package_tickets_ticket_type_id ON package_tickets(ticket_type_id);
-- Partial: package lines are the minority and the only rows the delete-guard scans.
CREATE INDEX idx_order_items_package_id         ON order_items(package_id) WHERE package_id IS NOT NULL;
CREATE INDEX idx_attendees_order_id             ON attendees(order_id);

COMMIT;
