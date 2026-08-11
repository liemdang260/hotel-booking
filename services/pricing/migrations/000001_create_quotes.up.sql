CREATE TABLE quotes (
    id TEXT PRIMARY KEY,
    hotel_id TEXT NOT NULL,
    room_type_id TEXT NOT NULL,
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    guest_count INTEGER NOT NULL CHECK (guest_count > 0),
    room_quantity INTEGER NOT NULL CHECK (room_quantity > 0),
    subtotal_minor BIGINT NOT NULL CHECK (subtotal_minor >= 0),
    tax_minor BIGINT NOT NULL CHECK (tax_minor >= 0),
    service_fee_minor BIGINT NOT NULL CHECK (service_fee_minor >= 0),
    discount_minor BIGINT NOT NULL CHECK (discount_minor >= 0),
    total_minor BIGINT NOT NULL CHECK (total_minor >= 0),
    currency CHAR(3) NOT NULL,
    pricing_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT quotes_valid_stay CHECK (check_out > check_in),
    CONSTRAINT quotes_valid_expiry CHECK (expires_at > created_at),
    CONSTRAINT quotes_total_matches CHECK (
        total_minor = subtotal_minor + tax_minor + service_fee_minor - discount_minor
    )
);

CREATE INDEX quotes_expires_at_idx ON quotes (expires_at);

CREATE FUNCTION reject_quote_update() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'quotes are immutable; create a new quote';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER quotes_are_immutable
BEFORE UPDATE ON quotes
FOR EACH ROW EXECUTE FUNCTION reject_quote_update();
