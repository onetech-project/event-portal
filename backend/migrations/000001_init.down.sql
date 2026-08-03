-- Reverses 000001_init.up.sql.
--
-- Tables are dropped in reverse dependency order so no FK ever blocks a DROP;
-- their indexes go with them, which is why only the tables are named here.

DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS attendees;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS ticket_types;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS admins;

-- Only safe because this migration is what created it and nothing outside this
-- schema depends on it.
DROP EXTENSION IF EXISTS "uuid-ossp";
