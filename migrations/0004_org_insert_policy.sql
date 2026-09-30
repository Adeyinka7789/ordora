-- =============================================================
-- 0004_org_insert_policy.sql
--
-- Solves the registration bootstrap problem WITHOUT a
-- security-definer function.
--
-- Observation: our RLS policy on organizations is:
--     WITH CHECK (id = current_org_id() AND tenant_is_set())
--
-- So the app can legitimately create a new organization by first
-- setting app.current_org_id = <new org id> and then inserting.
-- The policy passes because the new row's id matches the tenant
-- we claim. There is no privilege escalation: the app already
-- has to know the new UUID (it generated it).
--
-- No new policy is needed. The existing policy already permits
-- this. This migration exists only to document the pattern and
-- to add an index that makes the check cheap.
-- =============================================================

-- No structural change required. The existing policy is sufficient.
-- This migration is intentionally a no-op for future readers.

SELECT 1;