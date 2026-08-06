-- Admin-authored note shown on a ticket's booking card (spec 005, FR-040..FR-043).
--
-- It replaces the hardcoded "Tickets that have already been purchased are
-- non-refundable" line rather than sitting beside it: that string was baked into
-- the frontend, so organisers could not say anything ticket-specific there. NULL
-- or blank falls back to the original wording, which keeps every existing ticket
-- reading exactly as it did before this column existed.
--
-- packages.description already exists and is used the same way.

ALTER TABLE ticket_types ADD COLUMN description TEXT;
