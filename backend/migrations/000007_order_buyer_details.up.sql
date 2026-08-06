-- Buyer gender and date of birth (Figma 12-4456): the buyer form collects the
-- same personal fields as each visitor card. Nullable — pre-008 orders and
-- booked-but-never-checked-out orders have no buyer identity at all.
ALTER TABLE orders
    ADD COLUMN buyer_dob date,
    ADD COLUMN buyer_gender varchar(10);
