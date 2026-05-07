CREATE TYPE order_status AS ENUM ('pending', 'paid', 'fulfilled', 'shipped', 'completed', 'failed', 'cancelled');

CREATE TABLE IF NOT EXISTS orders (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    customer_email       TEXT NOT NULL,
    status               order_status NOT NULL DEFAULT 'pending',
    subtotal_amount      INTEGER NOT NULL DEFAULT 0,
    currency             TEXT NOT NULL DEFAULT 'AUD',
    shipping_amount      INTEGER NOT NULL DEFAULT 0,
    total_amount         INTEGER NOT NULL DEFAULT 0,
    shipping_name        TEXT NOT NULL,
    shipping_line1       TEXT NOT NULL,
    shipping_line2       TEXT NOT NULL DEFAULT '',
    shipping_city        TEXT NOT NULL,
    shipping_region      TEXT NOT NULL DEFAULT '',
    shipping_postal_code TEXT NOT NULL,
    shipping_country     TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT orders_subtotal_non_negative CHECK (subtotal_amount >= 0),
    CONSTRAINT orders_shipping_non_negative CHECK (shipping_amount >= 0),
    CONSTRAINT orders_total_non_negative CHECK (total_amount >= 0)
);

CREATE TABLE IF NOT EXISTS order_items (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id           UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    product_id         UUID NOT NULL,
    sku                TEXT NOT NULL,
    title              TEXT NOT NULL,
    quantity           INTEGER NOT NULL,
    unit_price_amount  INTEGER NOT NULL,
    currency           TEXT NOT NULL,
    line_total_amount  INTEGER NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT order_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT order_items_unit_price_non_negative CHECK (unit_price_amount >= 0),
    CONSTRAINT order_items_line_total_non_negative CHECK (line_total_amount >= 0)
);

CREATE TABLE IF NOT EXISTS carts (
    session_id      TEXT PRIMARY KEY,
    subtotal_amount INTEGER NOT NULL DEFAULT 0,
    currency        TEXT NOT NULL DEFAULT 'AUD',
    total_amount    INTEGER NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT carts_subtotal_non_negative CHECK (subtotal_amount >= 0),
    CONSTRAINT carts_total_non_negative CHECK (total_amount >= 0)
);

CREATE TABLE IF NOT EXISTS cart_items (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id         TEXT NOT NULL REFERENCES carts(session_id) ON DELETE CASCADE,
    product_id         UUID NOT NULL,
    sku                TEXT NOT NULL,
    title              TEXT NOT NULL,
    quantity           INTEGER NOT NULL,
    unit_price_amount  INTEGER NOT NULL,
    currency           TEXT NOT NULL,
    line_total_amount  INTEGER NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cart_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT cart_items_unit_price_non_negative CHECK (unit_price_amount >= 0),
    CONSTRAINT cart_items_line_total_non_negative CHECK (line_total_amount >= 0)
);

CREATE INDEX idx_orders_customer_email ON orders(customer_email);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created_at ON orders(created_at DESC);
CREATE INDEX idx_order_items_order_id ON order_items(order_id);
CREATE INDEX idx_cart_items_session_id ON cart_items(session_id);
