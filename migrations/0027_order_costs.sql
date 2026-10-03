-- =============================================================
-- 0027_order_costs.sql
--
-- Order costs are the expenses a business incurs fulfilling an
-- order: materials, labor, delivery, overhead, and so on.
--
-- They are internal-only. Never surfaced to customers, never
-- shown on the customer portal. RLS-protected like every other
-- tenant-owned table.
--
-- Design notes:
--   - Costs are soft-linked to an order. Delete an order → costs
--     cascade. Delete a cost → the order is unaffected.
--   - No receipt attachment. Businesses enter amounts freely.
--   - Currency must match the order's currency (enforced in the
--     domain, not the DB).
-- =============================================================

CREATE TYPE cost_category AS ENUM (
    'MATERIALS',
    'LABOR',
    'DELIVERY',
    'OVERHEAD',
    'SUBCONTRACTOR',
    'OTHER'
);

CREATE TABLE order_costs (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    category        cost_category NOT NULL,
    description     TEXT NOT NULL,
    amount_minor    BIGINT NOT NULL CHECK (amount_minor > 0),
    currency        CHAR(3) NOT NULL,
    incurred_on     DATE NOT NULL,
    vendor          TEXT,
    notes           TEXT,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_order_costs_order   ON order_costs(order_id);
CREATE INDEX idx_order_costs_org     ON order_costs(organization_id);
CREATE INDEX idx_order_costs_cat     ON order_costs(organization_id, category);
CREATE INDEX idx_order_costs_date    ON order_costs(organization_id, incurred_on);

-- RLS
ALTER TABLE order_costs ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_costs FORCE  ROW LEVEL SECURITY;

CREATE POLICY order_costs_tenant ON order_costs
    USING  (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

-- Grants for the app role.
GRANT SELECT, INSERT, UPDATE, DELETE ON order_costs TO ordora_app;
