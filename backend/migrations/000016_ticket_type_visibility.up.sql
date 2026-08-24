-- Spec 022: free ticket registration.
--
-- Two booleans, no index. `ticket_types.is_visible` defaults TRUE and
-- `orders.is_registration` defaults FALSE, so the migration is total with no
-- backfill statement and every pre-existing row keeps behaving exactly as it
-- did: every ticket type stays purchasable and listed, every order stays a
-- purchase (SC-009).
--
-- No explicit BEGIN/COMMIT: 000013, 000014 and 000015 omit it, and
-- golang-migrate wraps each postgres migration itself.

-- ticket_types.is_visible -----------------------------------------------------
--
-- READ THE DEFAULT TWICE. It is TRUE, and it has to be. An earlier draft of
-- this feature called the column `is_registration_only` with DEFAULT FALSE;
-- `is_visible` is its logical NEGATION, so copying that default across would
-- make every ticket type in the system invisible at once — every event's ticket
-- list emptied, every purchase refused — and raise no error anywhere. Spec 022
-- SC-009 exists to catch precisely that, and it can only catch it against a
-- database that already holds ticket types.
--
-- The name is narrower than what the column governs, and spec 022 FR-001a
-- requires that to be stated wherever the column is documented rather than left
-- to be discovered. FALSE does not merely hide the type. It means the type is
-- obtained by REGISTERING rather than by buying, and carries all three
-- behaviours: excluded from every guest purchase surface, refused by booking,
-- checkout and availability, and ineligible for package composition. There is
-- consequently no way to *just* hide a ticket type — making a purchasable type
-- invisible also makes it free to register for.
--
-- Deliberately NOT constrained against `price` (FR-003). A registration charges
-- nothing regardless of what is stored, so pricing such a type at zero is a
-- convention rather than an invariant; a CHECK here would encode a relationship
-- the spec explicitly rejects and would fail on any such type an admin prices
-- non-zero.
ALTER TABLE ticket_types
    ADD COLUMN is_visible BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN ticket_types.is_visible IS
    'FALSE means REGISTRATION-ONLY, not merely unlisted: obtained at /events/:slug/register/:id rather than by buying, hidden from every guest purchase surface, refused by booking, checkout and availability, and ineligible for package composition. There is no way to only hide a type — clearing this also makes it free to register for (spec 022 FR-001a). Independent of price (FR-003). Defaults TRUE; an inverted default would silently hide every ticket type in the system.';

-- orders: NO registration marker ----------------------------------------------
--
-- A draft of this migration added `orders.is_registration BOOLEAN NOT NULL
-- DEFAULT FALSE`, and Constitution Principle IV v5.0.0 required it: PAID had
-- gained a second origin, and the amendment demanded the two be distinguishable
-- in stored data.
--
-- That was reversed on 2026-08-20 by explicit decision, and the constitution was
-- amended with it (v6.0.0). The distinction is now DERIVED: an order is a
-- registration when it carries a line for a ticket type with `is_visible = FALSE`.
-- Containment guarantees such a type can never be bought or bundled, so one such
-- line can only have come from the registration path.
--
-- The trade was made with the cost stated, and the cost is recorded here rather
-- than in a commit message: a derived value is NOT STABLE OVER TIME. An admin who
-- makes an invitation ticket type purchasable again retroactively reclassifies
-- every historical order that used it — and a resend of a year-old registration
-- would then render a receipt for an order that never had a payment. A stored
-- marker could not do that. Nothing in the schema prevents it; only the admin's
-- restraint does.

-- No index is added by this migration.
--
-- An earlier draft added idx_attendees_email_lower for a per-address duplicate
-- check. That rule was removed (spec 022 FR-023: an address may register as many
-- times as quota allows), and with it the only read that needed the index. It is
-- not carried here for a reader who might want it later — an unused index is
-- write amplification on every attendee insert, which is the hot path this
-- feature adds load to.
