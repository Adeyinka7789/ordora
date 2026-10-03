-- =============================================================
-- 0024_org_suspension.sql
--
-- Adds suspension fields to organizations. A suspended org can
-- still log in, but is blocked from creating/editing business
-- data. Only platform admins can suspend or unsuspend.
-- =============================================================

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS suspended_at     TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS suspended_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_orgs_suspended
    ON organizations (suspended_at)
    WHERE suspended_at IS NOT NULL;
