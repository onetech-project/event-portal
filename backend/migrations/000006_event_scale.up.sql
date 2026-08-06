-- Spec 008 (clarification 2026-08-05, Figma 4-5): the detail page's info bar
-- carries a Scale cell — the expected visitor count, rendered by the client
-- as "30.000+ Visitors" / "1M+ Visitors". A number, not text, so the client
-- can compact-format it. Optional — NULL simply hides the cell.
ALTER TABLE events ADD COLUMN scale bigint;
