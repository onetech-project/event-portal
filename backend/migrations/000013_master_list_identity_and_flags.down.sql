-- Restores the pre-revision shape: uuid master-list keys, the name-based order
-- status and its FK, the packages status varchar + CHECK, and the original
-- partial index.
--
-- LOSSY IN ONE RESPECT, documented: the regenerated master-list uuids are NEW
-- values, not the originals. Nothing outside the database ever stored them —
-- the wire has only ever carried names (research R19) and `attendees.gender_id`
-- is remapped below — so no live reference breaks. A uuid captured in an old
-- log will not resolve. `created_by`/`updated_by` are dropped outright.

-- ---------------------------------------------------------------------------
-- 1. packages.is_active -> packages.status.
-- ---------------------------------------------------------------------------

ALTER TABLE packages ADD COLUMN status varchar(50) NOT NULL DEFAULT 'ACTIVE';
UPDATE packages SET status = CASE WHEN is_active THEN 'ACTIVE' ELSE 'INACTIVE' END;
ALTER TABLE packages
    ADD CONSTRAINT packages_status_check CHECK (status IN ('ACTIVE', 'INACTIVE'));
ALTER TABLE packages DROP COLUMN is_active;

-- ---------------------------------------------------------------------------
-- 2. Authorship and name widths.
-- ---------------------------------------------------------------------------

ALTER TABLE order_statuses DROP COLUMN created_by, DROP COLUMN updated_by;
ALTER TABLE genders DROP COLUMN created_by, DROP COLUMN updated_by;

ALTER TABLE order_statuses ALTER COLUMN name TYPE varchar(50);
ALTER TABLE genders ALTER COLUMN name TYPE varchar(50);

-- ---------------------------------------------------------------------------
-- 3. genders back to a uuid key, carrying attendees across by their own row.
-- ---------------------------------------------------------------------------

-- gen_random_uuid() is volatile, so PostgreSQL evaluates it per row: every
-- gender gets its own new uuid rather than one shared value.
ALTER TABLE genders ADD COLUMN old_id uuid NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE attendees ADD COLUMN old_gender_id uuid;
UPDATE attendees a SET old_gender_id = g.old_id
FROM genders g
WHERE g.id = a.gender_id;

ALTER TABLE attendees DROP COLUMN gender_id;
ALTER TABLE attendees RENAME COLUMN old_gender_id TO gender_id;

ALTER TABLE genders ALTER COLUMN id DROP IDENTITY;
ALTER TABLE genders DROP CONSTRAINT genders_pkey;
ALTER TABLE genders DROP COLUMN id;
ALTER TABLE genders RENAME COLUMN old_id TO id;
ALTER TABLE genders ADD PRIMARY KEY (id);

ALTER TABLE attendees
    ADD CONSTRAINT attendees_gender_id_fkey
    FOREIGN KEY (gender_id) REFERENCES genders (id);

-- ---------------------------------------------------------------------------
-- 4. orders.status_id -> orders.status, and order_statuses back to a uuid key.
--
-- The name is read back off the master list while the reference still exists.
-- ---------------------------------------------------------------------------

DROP INDEX IF EXISTS idx_orders_payment_expiry;

ALTER TABLE orders ADD COLUMN status varchar(50);
UPDATE orders o SET status = os.name
FROM order_statuses os
WHERE os.id = o.status_id;
ALTER TABLE orders ALTER COLUMN status SET NOT NULL;

ALTER TABLE orders DROP CONSTRAINT orders_status_id_fkey;
ALTER TABLE orders DROP COLUMN status_id;

ALTER TABLE order_statuses ALTER COLUMN id DROP IDENTITY;
ALTER TABLE order_statuses ADD COLUMN old_id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE order_statuses DROP CONSTRAINT order_statuses_pkey;
ALTER TABLE order_statuses DROP COLUMN id;
ALTER TABLE order_statuses RENAME COLUMN old_id TO id;
ALTER TABLE order_statuses ADD PRIMARY KEY (id);

-- ---------------------------------------------------------------------------
-- 5. The name-based FK and the original partial index (migrations 0009, 0002).
-- ---------------------------------------------------------------------------

ALTER TABLE orders
    ADD CONSTRAINT orders_status_fkey
    FOREIGN KEY (status) REFERENCES order_statuses (name);

CREATE INDEX idx_orders_payment_expiry
    ON orders (payment_expires_at)
    WHERE status = 'PENDING';
