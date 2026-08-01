-- 0002: in-app QRIS payment (specs/004-session-qris-payment).
--
-- The guest no longer leaves the site for the provider's hosted checkout, so the
-- order itself has to carry what the payment page renders: the QRIS payload and
-- the deadline it is valid until.
--
-- NOTE: docker-compose mounts this directory at /docker-entrypoint-initdb.d,
-- which Postgres runs ONLY when the data volume is empty. Apply this by hand to
-- any database that already exists:
--
--   psql "$DATABASE_URL" -f backend/migrations/0002_payment_qris.sql

-- The raw QRIS payload returned by the provider. The QR image is rendered from
-- this on demand at request time — this MVP has no object storage, so nothing is
-- ever written to disk (Constitution, Technology Stack Requirements).
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_qr_string TEXT;

-- Server-owned payment deadline. It drives the guest's countdown, the expiry
-- sweeper, and the decision to stop showing a code. Never computed from a client
-- clock.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS payment_expires_at TIMESTAMP WITH TIME ZONE;

-- The sweeper's only query is "PENDING orders whose deadline has passed". The
-- partial predicate keeps this index roughly the size of the live payment window
-- rather than the whole order history.
CREATE INDEX IF NOT EXISTS idx_orders_payment_expiry
    ON orders (payment_expires_at)
    WHERE status = 'PENDING';
