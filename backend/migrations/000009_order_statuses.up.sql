-- Order-status master table (clarified 2026-08-05): the set of statuses is
-- data, not a CHECK constraint baked into the orders table. orders.status
-- keeps its varchar value — every query that filters on status = 'PENDING'
-- is untouched — but the value is now foreign-keyed to this list, so the
-- master table is authoritative.
CREATE TABLE order_statuses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(50) NOT NULL UNIQUE,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz
);

INSERT INTO order_statuses (name)
VALUES ('PENDING'), ('PAID'), ('CANCELLED'), ('EXPIRED');

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_fkey
    FOREIGN KEY (status) REFERENCES order_statuses (name);
