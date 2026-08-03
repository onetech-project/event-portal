-- Reverses 000002_payment_qris.up.sql.
--
-- Destructive: every QRIS payload and payment deadline recorded so far is lost.
-- Any order still PENDING when this runs loses the deadline the sweeper expires
-- it by, so it will hold its quota until an admin cancels it by hand.

DROP INDEX IF EXISTS idx_orders_payment_expiry;

ALTER TABLE orders DROP COLUMN IF EXISTS payment_expires_at;
ALTER TABLE orders DROP COLUMN IF EXISTS payment_qr_string;
