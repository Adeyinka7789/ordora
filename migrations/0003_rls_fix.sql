-- =============================================================
-- 0003_rls_fix.sql
-- Fixes the NULL-in-policy bug discovered during RLS testing.
--
-- Problem: when app.current_org_id is unset, the helper returned
-- NULL. Policies compared column = NULL, which evaluates to NULL
-- (unknown), and Postgres allowed some INSERTs through.
--
-- Fix: the helper returns a sentinel UUID (all zeros) when the
-- setting is unset. No real row ever has that id, so all policies
-- produce a definite FALSE and access is denied.
-- =============================================================

CREATE OR REPLACE FUNCTION current_org_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        NULLIF(current_setting('app.current_org_id', true), '')::uuid,
        '00000000-0000-0000-0000-000000000000'::uuid
    )
$$;

-- Also expose a helper that tells callers whether a tenant is set at all.
-- Useful for debugging and for policies that need to distinguish
-- "no tenant" from "wrong tenant".
CREATE OR REPLACE FUNCTION tenant_is_set() RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(NULLIF(current_setting('app.current_org_id', true), ''), '') <> ''
$$;

-- Recreate every tenant policy so the WITH CHECK is also explicit.
-- (The USING clause is what matters for reads; the WITH CHECK is
-- what matters for writes. We make both rely on the fixed helper.)

DROP POLICY IF EXISTS customers_tenant ON customers;
CREATE POLICY customers_tenant ON customers
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS orders_tenant ON orders;
CREATE POLICY orders_tenant ON orders
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS order_items_tenant ON order_items;
CREATE POLICY order_items_tenant ON order_items
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS payments_tenant ON payments;
CREATE POLICY payments_tenant ON payments
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS attachments_tenant ON attachments;
CREATE POLICY attachments_tenant ON attachments
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS audit_logs_tenant ON audit_logs;
CREATE POLICY audit_logs_tenant ON audit_logs
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS outbox_tenant ON outbox;
CREATE POLICY outbox_tenant ON outbox
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS notification_jobs_tenant ON notification_jobs;
CREATE POLICY notification_jobs_tenant ON notification_jobs
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS organization_members_tenant ON organization_members;
CREATE POLICY organization_members_tenant ON organization_members
    USING  (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

DROP POLICY IF EXISTS organizations_tenant ON organizations;
CREATE POLICY organizations_tenant ON organizations
    USING  (id = current_org_id() AND tenant_is_set())
    WITH CHECK (id = current_org_id() AND tenant_is_set());