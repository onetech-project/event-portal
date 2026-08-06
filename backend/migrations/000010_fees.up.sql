-- Fee master + per-order snapshot (clarified 2026-08-05, Figma 32-1366).
--
-- `fees` is what the admin edits: a PERCENT fee is applied to the order's
-- subtotal, a FIXED fee is a flat rupiah amount. `order_fees` freezes the
-- computed lines onto the order at booking, so a later fee edit never changes
-- what an existing order shows or charged.
CREATE TABLE fees (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(100) NOT NULL UNIQUE,
    fee_type varchar(10) NOT NULL CHECK (fee_type IN ('PERCENT', 'FIXED')),
    value numeric(12,2) NOT NULL CHECK (value >= 0),
    position int NOT NULL DEFAULT 0,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz
);

INSERT INTO fees (name, fee_type, value, position)
VALUES ('PPN', 'PERCENT', 11.00, 1), ('Admin Fee', 'FIXED', 1200.00, 2);

-- The pre-fee sum of the order's lines. NULL on orders that predate fees;
-- total_amount remains the single charged amount everywhere.
ALTER TABLE orders ADD COLUMN subtotal numeric(12,2);

CREATE TABLE order_fees (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id uuid NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    -- Display name as computed at booking, e.g. 'PPN (11%)' — a snapshot,
    -- deliberately not a foreign key onto the editable master row.
    name varchar(120) NOT NULL,
    amount numeric(12,2) NOT NULL,
    position int NOT NULL DEFAULT 0
);

CREATE INDEX idx_order_fees_order ON order_fees (order_id);
