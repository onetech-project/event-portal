-- Event CMS content + two-phase booking (spec 008).
--
-- Four content tables owned by the event domain, all ON DELETE CASCADE: they are
-- presentation data and must not block event deletion the way orders do. Orders
-- gain a durable T&C agreement record; attendees become bookable as EMPTY SLOTS
-- at reservation time and are filled with visitor details only at checkout
-- (clarification 2026-08-05, Option B), so their identity columns relax to NULL.

BEGIN;

-- 11. EVENT_TERMS — one live Terms & Conditions document per event, authored in
-- the admin WYSIWYG. Content is sanitized HTML: the API sanitizes on write, so
-- every read path can render it verbatim.
CREATE TABLE event_terms (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 12. EVENT_ACTIVITIES — "what's on" blocks on the event detail page.
-- icon is a NAMED KEY from a fixed client-side set, never a URL: this MVP has no
-- object storage, so nothing is uploaded or hosted.
CREATE TABLE event_activities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    icon VARCHAR(100),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 13. EVENT_GUEST_STARS — lineup names on the event detail page.
CREATE TABLE event_guest_stars (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 14. EVENT_GUIDELINES — visitor do/don't rows on the event detail page.
CREATE TABLE event_guidelines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    description VARCHAR(500) NOT NULL,
    icon VARCHAR(100),
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_event_terms_event_id ON event_terms(event_id);
CREATE INDEX idx_event_activities_event_id ON event_activities(event_id);
CREATE INDEX idx_event_guest_stars_event_id ON event_guest_stars(event_id);
CREATE INDEX idx_event_guidelines_event_id ON event_guidelines(event_id);

-- ORDERS: the durable T&C agreement record (FR-008). terms_agreed_at is stamped
-- by the record-agreement call fired on the same "Agree" click as booking;
-- checkout refuses orders where it is still NULL. Buyer identity arrives only at
-- checkout now, so the booking INSERT needs the columns nullable.
ALTER TABLE orders
    ADD COLUMN terms_agreed_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN event_terms_id UUID REFERENCES event_terms(id) ON DELETE SET NULL,
    ALTER COLUMN buyer_name DROP NOT NULL,
    ALTER COLUMN buyer_email DROP NOT NULL,
    ALTER COLUMN buyer_phone DROP NOT NULL;

-- ATTENDEES: booked as empty slots (ticket-type-bound) inside the reservation
-- transaction; visitor details land at checkout. Completeness is enforced by the
-- order service before payment starts — the schema must allow the empty phase.
ALTER TABLE attendees
    ALTER COLUMN name DROP NOT NULL,
    ALTER COLUMN email DROP NOT NULL,
    ADD COLUMN phone VARCHAR(50),
    ADD COLUMN dob DATE,
    ADD COLUMN gender VARCHAR(20) CHECK (gender IN ('MALE', 'FEMALE'));

-- Backfill: every existing event gets the previously hard-coded terms (retired
-- from frontend/lib/terms.ts) so the T&C dialog never goes blank and booking is
-- never refused for a pre-existing event.
INSERT INTO event_terms (event_id, content)
SELECT
    e.id,
    '<ol>'
    || '<li>All ticket sales are final. Tickets that have been purchased cannot be canceled, refunded, or transferred to another party.</li>'
    || '<li>Each ticket is valid for 1 (one) person only according to the registered data. The data filled in at the time of purchase must be accurate and match the identity of the ticket holder.</li>'
    || '<li>Visitors must be at least 17 (seventeen) years old at the time of attending the event.</li>'
    || '<li class="font-bold">In order for Tickets to be valid on event day, the Ticket Buyer will be asked to provide the following items :'
    || '<ul><li>Photo ID (ID Card/KK/KTP/SIM/KTM/KIA/Passport)</li><li>E-Voucher</li></ul></li>'
    || '<li>Visitors must comply with all rules and regulations applicable in the event area. The Organizer reserves the right to refuse entry or remove visitors who violate rules, disturb order, or endanger others.</li>'
    || '<li>Forging, duplicating, or misusing tickets in any form is strictly prohibited.</li>'
    || '<li>The event schedule, lineup, and rundown of activities are subject to change at any time according to the Organizer''s policy without prior notice.</li>'
    || '</ol>'
FROM events e;

COMMIT;
