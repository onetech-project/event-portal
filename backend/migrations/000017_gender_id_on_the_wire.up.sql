-- Spec 011 FR-034 / spec 022 FR-022 (clarified 2026-08-24): a holder form and a
-- registration form now submit the gender master entry's IDENTIFIER, not its
-- display name.
--
-- No column, constraint or index changes. `attendees.gender_id` was already the
-- storage; what changed is the wire in front of it, and this migration exists
-- solely so the column COMMENT stops asserting the opposite. That comment is
-- read by anyone inspecting the schema directly, and leaving it saying "the API
-- keeps exchanging the gender NAME" would make the database itself the most
-- authoritative-looking wrong answer in the repository.

COMMENT ON COLUMN attendees.gender_id IS
    'FK to genders(id). NULL while the slot is unfilled (pre-checkout). The API exchanges the gender ID: a form submits gender_id, and the order readback carries both gender_id and the display name, because a retired entry is absent from the active master list and could otherwise be resolved from neither direction (spec 011 FR-034/FR-035, 2026-08-24).';
