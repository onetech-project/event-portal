-- Reverse of 000005. DESTRUCTIVE for booking-phase rows: restoring NOT NULL on
-- buyer/attendee identity requires filling placeholders into rows created as
-- empty slots — those placeholders are fabricated, not recovered data.

BEGIN;

ALTER TABLE attendees
    DROP COLUMN IF EXISTS gender,
    DROP COLUMN IF EXISTS dob,
    DROP COLUMN IF EXISTS phone;

UPDATE attendees SET name = '' WHERE name IS NULL;
UPDATE attendees SET email = '' WHERE email IS NULL;
ALTER TABLE attendees
    ALTER COLUMN name SET NOT NULL,
    ALTER COLUMN email SET NOT NULL;

UPDATE orders SET buyer_name = '' WHERE buyer_name IS NULL;
UPDATE orders SET buyer_email = '' WHERE buyer_email IS NULL;
UPDATE orders SET buyer_phone = '' WHERE buyer_phone IS NULL;
ALTER TABLE orders
    ALTER COLUMN buyer_name SET NOT NULL,
    ALTER COLUMN buyer_email SET NOT NULL,
    ALTER COLUMN buyer_phone SET NOT NULL,
    DROP COLUMN IF EXISTS event_terms_id,
    DROP COLUMN IF EXISTS terms_agreed_at;

DROP TABLE IF EXISTS event_guidelines;
DROP TABLE IF EXISTS event_guest_stars;
DROP TABLE IF EXISTS event_activities;
DROP TABLE IF EXISTS event_terms;

COMMIT;
