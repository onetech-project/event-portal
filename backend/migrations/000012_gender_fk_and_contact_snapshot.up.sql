-- Spec 011: holder gender becomes a real reference to the genders master table
-- (FR-018), and the orders buyer snapshot narrows to what downstream actually
-- consumes (name/email/phone — payment customer details, admin list, delivery
-- fallback). buyer_dob/buyer_gender were write-only mirrors of the now-removed
-- buyer form (spec 008 Figma 12-4456); the holder's dob/gender live on their
-- attendee row.
ALTER TABLE attendees
    ADD COLUMN gender_id uuid REFERENCES genders(id);

COMMENT ON COLUMN attendees.gender_id IS
    'FK to genders(id). NULL while the slot is unfilled (pre-checkout). The API keeps exchanging the gender NAME; the order service maps name <-> id.';

-- Total backfill: the old CHECK admitted only MALE/FEMALE, both seeded in the
-- genders master (migration 0008), so no row can be left behind.
UPDATE attendees a
SET gender_id = g.id
FROM genders g
WHERE a.gender = g.name;

ALTER TABLE attendees
    DROP COLUMN gender; -- the CHECK (gender IN ('MALE','FEMALE')) dies with it

ALTER TABLE orders
    DROP COLUMN buyer_dob,
    DROP COLUMN buyer_gender;
