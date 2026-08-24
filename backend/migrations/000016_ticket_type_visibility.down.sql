-- Reverses 000016. The COMMENT ON COLUMN entry is dropped with its column.
ALTER TABLE ticket_types
    DROP COLUMN is_visible;
