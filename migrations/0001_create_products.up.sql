CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE product_status AS ENUM ('draft', 'active', 'archived');

CREATE TABLE IF NOT EXISTS categories (
    id   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS products (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sku            TEXT NOT NULL UNIQUE,
    title          TEXT NOT NULL,
    slug           TEXT NOT NULL UNIQUE,
    description    TEXT NOT NULL DEFAULT '',
    price_amount   INTEGER NOT NULL DEFAULT 0,
    price_currency TEXT NOT NULL DEFAULT 'AUD',
    stock          INTEGER NOT NULL DEFAULT 0,
    status         product_status NOT NULL DEFAULT 'draft',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT products_price_amount_non_negative CHECK (price_amount >= 0),
    CONSTRAINT products_stock_non_negative CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS product_images (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    alt        TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS product_categories (
    product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, category_id)
);

CREATE INDEX idx_categories_slug ON categories(slug);
CREATE INDEX idx_products_status ON products(status);
CREATE INDEX idx_products_slug ON products(slug);
CREATE INDEX idx_products_created_at ON products(created_at DESC);
CREATE INDEX idx_product_categories_category ON product_categories(category_id);
CREATE INDEX idx_product_images_product_id ON product_images(product_id, sort_order);
