-- =============================================================
-- 0002_rls.sql
-- Enables Row-Level Security on every tenant-owned table.
-- Every policy reads app.current_org_id (set per-transaction by the app).
-- =============================================================

-- Helper: the current tenant. Reads the session variable set by the app.
-- Returns NULL if unset, which makes every policy deny access.
CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT NULLIF(current_setting('app.current_org_id', true), '')::uuid
$$;

-- -------------------------------------------------------------
-- Tables that have a direct organization_id column:
-- enable RLS, force it, and apply the standard tenant policy.
-- -------------------------------------------------------------

-- customers
ALTER TABLE customers ENABLE ROW LEVEL SECURITY;
ALTER TABLE customers FORCE  ROW LEVEL SECURITY;
CREATE POLICY customers_tenant ON customers
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- orders
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders FORCE  ROW LEVEL SECURITY;
CREATE POLICY orders_tenant ON orders
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- order_items
ALTER TABLE order_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_items FORCE  ROW LEVEL SECURITY;
CREATE POLICY order_items_tenant ON order_items
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- payments
ALTER TABLE payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE payments FORCE  ROW LEVEL SECURITY;
CREATE POLICY payments_tenant ON payments
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- attachments
ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
ALTER TABLE attachments FORCE  ROW LEVEL SECURITY;
CREATE POLICY attachments_tenant ON attachments
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- audit_logs
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE  ROW LEVEL SECURITY;
CREATE POLICY audit_logs_tenant ON audit_logs
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- outbox
ALTER TABLE outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox FORCE  ROW LEVEL SECURITY;
CREATE POLICY outbox_tenant ON outbox
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- notification_jobs
ALTER TABLE notification_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_jobs FORCE  ROW LEVEL SECURITY;
CREATE POLICY notification_jobs_tenant ON notification_jobs
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- organization_members: tenant scoped via organization_id.
-- -------------------------------------------------------------
ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_members FORCE  ROW LEVEL SECURITY;
CREATE POLICY organization_members_tenant ON organization_members
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- organizations: readable only when it's the current tenant.
-- -------------------------------------------------------------
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE  ROW LEVEL SECURITY;
CREATE POLICY organizations_tenant ON organizations
    USING (id = current_org_id())
    WITH CHECK (id = current_org_id());

-- -------------------------------------------------------------
-- Tables deliberately NOT tenant-scoped (no RLS):
--   users           -- a user can belong to multiple orgs later
--   sessions        -- session is per-user, established before tenant context
--   auth_tokens     -- pre-auth; user identity only
--
-- These are accessed only by the auth layer, never from tenant-scoped
-- code paths, and never returned in tenant responses.
-- -------------------------------------------------------------