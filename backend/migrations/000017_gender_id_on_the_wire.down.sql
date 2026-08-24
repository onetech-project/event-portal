-- Restores the comment migration 000013 left on the column. The wire contract
-- itself is application code and is not reverted by this file.
COMMENT ON COLUMN attendees.gender_id IS
    'FK to genders(id). NULL while the slot is unfilled (pre-checkout). The API keeps exchanging the gender NAME; the order service maps name <-> id.';
