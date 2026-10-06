-- =============================================================
-- 0044_order_groups.sql
--
-- Aso-ebi / bulk group orders: one style, many members. A group is a
-- named collection of orders (one per member) with an optional
-- occasion date (the party). Per-member measurements, items, payments
-- and balances all live on the member orders — the group only groups.
-- =============================================================

CREATE TABLE order_groups (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL CHECK (name <> ''),
    occasion_date   DATE,
    notes           TEXT NOT NULL DEFAULT '',
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_order_groups_org ON order_groups(organization_id);
CREATE INDEX IF NOT EXISTS idx_order_groups_occasion ON order_groups(organization_id, occasion_date);

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES order_groups(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_orders_group ON orders(group_id);

-- RLS: tenant-scoped like every other tenant table.
ALTER TABLE order_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_groups FORCE ROW LEVEL SECURITY;

CREATE POLICY order_groups_tenant ON order_groups
    USING      (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

GRANT SELECT, INSERT, UPDATE, DELETE ON order_groups TO ordora_app;

INSERT INTO admin_browsable_tables (table_name, display_name, search_columns, order_by, order_dir) VALUES
    ('order_groups', 'Order Groups', ARRAY['name'], 'created_at', 'DESC')
ON CONFLICT (table_name) DO NOTHING;
