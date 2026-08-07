-- Spec 011 rev. 3 (FR-025 – FR-029): the master lists move from random uuid
-- keys to compact auto-assigned integers, gain authorship columns, the order's
-- status becomes a reference rather than a stored word, and a package's
-- availability becomes a boolean.
--
-- What does NOT change: `tickets.status` keeps its three-state ACTIVE/USED/
-- REVOKED lifecycle (FR-029 excludes it by name — the gate must tell a pass
-- already scanned apart from one revoked, which a boolean cannot express), and
-- `events.status` (DRAFT/PUBLISHED/COMPLETED) is a different column entirely.
--
-- Nothing above storage moves: every read below re-exposes the status NAME, so
-- the wire contract, the DTOs, and all 29 files that compare the status as a Go
-- string are untouched (research R19).
--
-- The step order here is forced by PostgreSQL, not chosen. See §7.2 of
-- data-model.md; each constraint is noted inline.

-- ---------------------------------------------------------------------------
-- 1. Clear the two things that depend on `orders.status` before it moves.
-- ---------------------------------------------------------------------------

-- Rebuilt in step 6 against the new column. A partial index predicate cannot
-- survive the column it names being dropped.
DROP INDEX IF EXISTS idx_orders_payment_expiry;

-- The name-based FK from migration 0009. It points at `order_statuses.name`;
-- the replacement points at the id, so it must go before the key changes.
ALTER TABLE orders DROP CONSTRAINT orders_status_fkey;

-- ---------------------------------------------------------------------------
-- 2. order_statuses: uuid key -> integer identity, at FIXED ids.
--
-- The ids are assigned explicitly rather than left to insertion order because
-- step 6's partial index has to name one of them as a literal: PostgreSQL
-- requires an immutable predicate and rejects a subquery outright. That makes
-- `1 = PENDING` part of the schema contract (research R20).
-- ---------------------------------------------------------------------------

ALTER TABLE order_statuses ADD COLUMN new_id integer;

UPDATE order_statuses SET new_id = CASE name
    WHEN 'PENDING'   THEN 1
    WHEN 'PAID'      THEN 2
    WHEN 'CANCELLED' THEN 3
    WHEN 'EXPIRED'   THEN 4
END;

-- Defensive: no admin CRUD for this table exists today, so the four seeded rows
-- are all there can be. Should any other row exist, it gets a stable id after
-- the reserved four rather than a NULL that fails the NOT NULL below.
UPDATE order_statuses os SET new_id = 4 + ranked.rn
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, name) AS rn
    FROM order_statuses
    WHERE name NOT IN ('PENDING', 'PAID', 'CANCELLED', 'EXPIRED')
) ranked
WHERE os.id = ranked.id;

ALTER TABLE order_statuses ALTER COLUMN new_id SET NOT NULL;

-- ---------------------------------------------------------------------------
-- 3. orders.status (name) -> orders.status_id (reference).
--
-- Populated by joining the OLD name while it is still there. `integer`, not
-- `smallint`, so the FK column and `order_statuses.id` are the same type.
-- ---------------------------------------------------------------------------

ALTER TABLE orders ADD COLUMN status_id integer;

UPDATE orders o SET status_id = os.new_id
FROM order_statuses os
WHERE os.name = o.status;

ALTER TABLE orders ALTER COLUMN status_id SET NOT NULL;
ALTER TABLE orders DROP COLUMN status;

-- ---------------------------------------------------------------------------
-- 4. Swap order_statuses onto its new key.
-- ---------------------------------------------------------------------------

ALTER TABLE order_statuses DROP CONSTRAINT order_statuses_pkey;
ALTER TABLE order_statuses DROP COLUMN id;
ALTER TABLE order_statuses RENAME COLUMN new_id TO id;
ALTER TABLE order_statuses ADD PRIMARY KEY (id);
ALTER TABLE order_statuses ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY;

-- The identity sequence starts at 1, which would collide with the rows just
-- seeded. Move it past them.
SELECT setval(
    pg_get_serial_sequence('order_statuses', 'id'),
    (SELECT COALESCE(MAX(id), 1) FROM order_statuses)
);

-- ---------------------------------------------------------------------------
-- 5. The new reference.
-- ---------------------------------------------------------------------------

ALTER TABLE orders
    ADD CONSTRAINT orders_status_id_fkey
    FOREIGN KEY (status_id) REFERENCES order_statuses (id);

-- ---------------------------------------------------------------------------
-- 6. Rebuild the sweeper's partial index on the literal id.
--
-- Not optional: the expiry sweep scans pending orders by payment_expires_at on
-- every tick, and without this it degrades to a sequential scan over the whole
-- order history.
-- ---------------------------------------------------------------------------

CREATE INDEX idx_orders_payment_expiry
    ON orders (payment_expires_at)
    WHERE status_id = 1; -- PENDING, fixed in step 2

-- ---------------------------------------------------------------------------
-- 7. genders: uuid key -> smallint identity, with attendees carried across.
--
-- Each attendee's new gender is derived from ITS OWN old uuid, never from row
-- order — that is what makes FR-025's "no reference silently re-bound to a
-- different entry" checkable rather than hoped for. There is no uuid->integer
-- cast, so add/populate/drop is the only shape available.
-- ---------------------------------------------------------------------------

ALTER TABLE genders ADD COLUMN new_id smallint;

UPDATE genders g SET new_id = ranked.rn
FROM (
    SELECT id, row_number() OVER (ORDER BY created_at, name) AS rn
    FROM genders
) ranked
WHERE g.id = ranked.id;

ALTER TABLE genders ALTER COLUMN new_id SET NOT NULL;

-- Nullable throughout: a slot created at booking has no holder yet.
ALTER TABLE attendees ADD COLUMN new_gender_id smallint;

UPDATE attendees a SET new_gender_id = g.new_id
FROM genders g
WHERE g.id = a.gender_id;

-- Drops the old FK to genders(id) along with the column.
ALTER TABLE attendees DROP COLUMN gender_id;
ALTER TABLE attendees RENAME COLUMN new_gender_id TO gender_id;

ALTER TABLE genders DROP CONSTRAINT genders_pkey;
ALTER TABLE genders DROP COLUMN id;
ALTER TABLE genders RENAME COLUMN new_id TO id;
ALTER TABLE genders ADD PRIMARY KEY (id);
ALTER TABLE genders ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY;

SELECT setval(
    pg_get_serial_sequence('genders', 'id'),
    (SELECT COALESCE(MAX(id), 1) FROM genders)
);

ALTER TABLE attendees
    ADD CONSTRAINT attendees_gender_id_fkey
    FOREIGN KEY (gender_id) REFERENCES genders (id);

COMMENT ON COLUMN attendees.gender_id IS
    'FK to genders(id). NULL while the slot is unfilled (pre-checkout). The API keeps exchanging the gender NAME; the order service maps name <-> id.';

-- ---------------------------------------------------------------------------
-- 8. Master-list name widths (FR-027).
--
-- NOT NULL and UNIQUE were already in force from migrations 0008/0009 and stay
-- that way: both the gender a form submits and the order status carried on the
-- wire are resolved to their row BY NAME, so a duplicate or absent name would
-- leave that lookup with no single answer.
-- ---------------------------------------------------------------------------

ALTER TABLE order_statuses ALTER COLUMN name TYPE varchar(256);
ALTER TABLE genders ALTER COLUMN name TYPE varchar(100);

-- ---------------------------------------------------------------------------
-- 9. Authorship (FR-028).
--
-- Backfilled BEFORE the NOT NULL, or the constraint fails on existing rows.
-- 'SYSTEM' is a reserved literal, not an account: nothing authenticates as it.
-- The other branch of FR-028 — recording the acting admin — has no code path
-- yet, because no admin CRUD exists for either master list. It activates when
-- that CRUD is built (research R22).
-- ---------------------------------------------------------------------------

ALTER TABLE order_statuses
    ADD COLUMN created_by varchar(50),
    ADD COLUMN updated_by varchar(50);
UPDATE order_statuses SET created_by = 'SYSTEM' WHERE created_by IS NULL;
ALTER TABLE order_statuses ALTER COLUMN created_by SET NOT NULL;

ALTER TABLE genders
    ADD COLUMN created_by varchar(50),
    ADD COLUMN updated_by varchar(50);
UPDATE genders SET created_by = 'SYSTEM' WHERE created_by IS NULL;
ALTER TABLE genders ALTER COLUMN created_by SET NOT NULL;

-- ---------------------------------------------------------------------------
-- 10. packages.status -> packages.is_active (FR-029).
--
-- Unlike the order status, this one surfaces on the wire as a boolean too: a
-- package flag has two states and no master list behind it, so a boolean says
-- everything the strings did.
-- ---------------------------------------------------------------------------

ALTER TABLE packages ADD COLUMN is_active boolean NOT NULL DEFAULT true;
UPDATE packages SET is_active = (status = 'ACTIVE');
ALTER TABLE packages DROP COLUMN status; -- its CHECK dies with the column
