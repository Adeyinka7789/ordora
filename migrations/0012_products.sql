-- =============================================================
-- 0012_products.sql
--
-- A product is a reusable, priced item in a business's catalog.
-- When an order is created from a product, the price is copied
-- onto the order item (snapshot) — never referenced. This keeps
-- historical orders stable when a product's price changes.
--
-- Products can be archived (active = false) instead of deleted,
-- so old orders that reference them by name still make sense.
-- =============================================================

CREATE TABLE products (
    id                UUID PRIMARY KEY,
    organization_id   UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    description       TEXT,
    sku               TEXT,
    unit_price_minor  BIGINT NOT NULL CHECK (unit_price_minor >= 0),
    currency          CHAR(3) NOT NULL,
    active            BOOLEAN NOT NULL DEFAULT true,
    created_by        UUID NOT NULL REFERENCES users(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_org_active ON products(organization_id) WHERE active = true;
CREATE INDEX idx_products_org_name   ON products(organization_id, name);

-- Trigram index for name search (same pattern as customers).
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_products_org_name_trgm
    ON products USING gin (name gin_trgm_ops);

-- RLS
ALTER TABLE products ENABLE ROW LEVEL SECURITY;
ALTER TABLE products FORCE  ROW LEVEL SECURITY;

CREATE POLICY products_tenant ON products
    USING  (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

-- Grants for the app role.
GRANT SELECT, INSERT, UPDATE, DELETE ON products TO ordora_app;
