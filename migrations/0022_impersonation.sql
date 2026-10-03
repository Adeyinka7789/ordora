-- =============================================================
-- 0022_impersonation.sql
--
-- Admin impersonation lets a platform admin sign in as a business
-- owner to debug or support them. Every impersonation session is
-- time-limited and logged.
-- =============================================================

CREATE TABLE impersonation_sessions (
    id              UUID PRIMARY KEY,
    admin_id        UUID NOT NULL REFERENCES platform_admins(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash      BYTEA NOT NULL UNIQUE,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_impersonation_admin ON impersonation_sessions (admin_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_impersonation_expires ON impersonation_sessions (expires_at) WHERE revoked_at IS NULL;
