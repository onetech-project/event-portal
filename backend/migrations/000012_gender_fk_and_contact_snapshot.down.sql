-- Restores the pre-011 shape. LOSSY: orders.buyer_dob/buyer_gender come back
-- empty (their values are reconstructible from the primary holder's attendee
-- row, which spec 011 makes the source of truth).
ALTER TABLE orders
    ADD COLUMN buyer_dob date,
    ADD COLUMN buyer_gender varchar(10);

ALTER TABLE attendees
    ADD COLUMN gender varchar(20);

UPDATE attendees a
SET gender = g.name
FROM genders g
WHERE a.gender_id = g.id;

ALTER TABLE attendees
    ADD CONSTRAINT attendees_gender_check CHECK (gender IN ('MALE', 'FEMALE'));

ALTER TABLE attendees
    DROP COLUMN gender_id;
