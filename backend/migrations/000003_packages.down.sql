-- Reverses 000003_packages.up.sql in FK-safe order.

BEGIN;

DROP INDEX IF EXISTS idx_attendees_order_id;
DROP INDEX IF EXISTS idx_order_items_package_id;
DROP INDEX IF EXISTS idx_package_tickets_ticket_type_id;
DROP INDEX IF EXISTS idx_package_tickets_package_id;
DROP INDEX IF EXISTS idx_packages_event_id;

ALTER TABLE attendees DROP COLUMN IF EXISTS package_id;

ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_quantity_chk;
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_line_kind_chk;
-- Only succeeds while no package lines exist. If any do, this fails here rather than
-- silently discarding what those lines recorded.
ALTER TABLE order_items ALTER COLUMN ticket_type_id SET NOT NULL;
ALTER TABLE order_items DROP COLUMN IF EXISTS package_id;

DROP TABLE IF EXISTS package_tickets;
DROP TABLE IF EXISTS packages;

ALTER TABLE ticket_types DROP CONSTRAINT IF EXISTS ticket_types_id_event_uk;

COMMIT;
