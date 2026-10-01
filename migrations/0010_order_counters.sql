-- =============================================================
-- 0010_order_counters.sql
--
-- Per-organization, per-year order number counters.
--
-- Design: a dedicated counters table rather than Postgres SEQUENCE,
-- because sequences are global (not per-tenant) and do not roll
-- back (leaving gaps in order numbers).
--
-- Concurrency: allocation uses SELECT ... FOR UPDATE inside the
-- same transaction as the order insert. Two concurrent order
-- creations for the same org serialize on the counter row, which
-- is exactly the desired behavior. Different orgs never contend.
--
-- RLS: this table is tenant-scoped, keyed on organization_id.
-- =============================================================

CREATE TABLE order_counters (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    year            INT  NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    last_sequence   BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, year)
);

-- RLS: standard tenant policy.
ALTER TABLE order_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_counters FORCE  ROW LEVEL SECURITY;

CREATE POLICY order_counters_tenant ON order_counters
    USING  (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());
