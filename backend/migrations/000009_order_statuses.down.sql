ALTER TABLE orders DROP CONSTRAINT orders_status_fkey;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('PENDING', 'PAID', 'CANCELLED', 'EXPIRED'));
DROP TABLE order_statuses;
