-- Gender master table (clarified 2026-08-05): the forms' gender options come
-- from here rather than a hardcoded list, so the set is data, not code. `name`
-- is the canonical value stored on orders.buyer_gender and attendees.gender.
CREATE TABLE genders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(50) NOT NULL UNIQUE,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz
);

INSERT INTO genders (name) VALUES ('FEMALE'), ('MALE');
