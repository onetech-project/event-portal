-- Bundle-unit provenance (spec 010): booking stamps which purchased bundle
-- unit each slot belongs to, so one visitor form can fill a whole unit and the
-- server can require identical visitor data within it. NULL for standalone
-- slots and for bundle slots booked before this column existed (those keep the
-- pre-010 one-form-per-slot behavior).
ALTER TABLE attendees
    ADD COLUMN package_unit smallint
        CHECK (package_unit IS NULL OR package_unit >= 1);

COMMENT ON COLUMN attendees.package_unit IS
    'Ordinal of the purchased bundle unit this slot belongs to (1..quantity of its package order line). NULL for standalone-ticket slots and for bundle slots booked before spec 010.';
