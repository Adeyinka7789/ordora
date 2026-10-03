-- =============================================================
-- 0021_platform_admins.sql
--
-- Platform admins are a separate identity from business users.
-- They log in at a configurable admin path, use a separate
-- session cookie, and every action is written to a dedicated
-- audit log.
--
-- Design notes:
--   - TOTP secret is nullable (2FA is optional in V1, mandatory in V2)
--   - No email uniqueness constraint on the business `users` table —
--     a person could be a business user and an admin with different
--     accounts, which is intentional.
-- =============================================================

CREATE TABLE platform_admins (
    id              UUID PRIMARY KEY,
    email           CITEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    name            TEXT NOT NULL,
    totp_secret     TEXT,              -- nullable; set when 2FA is enabled
    totp_enabled_at TIMESTAMPTZ,       -- null means 2FA not enabled
    last_login_at   TIMESTAMPTZ,
    last_login_ip   INET,
    disabled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_platform_admins_active
    ON platform_admins (email)
    WHERE disabled_at IS NULL;

-- Admin sessions are tracked in the same sessions table as business
-- sessions, but with a different cookie and a marker. Actually, to keep
-- separation clean and avoid any chance of cross-contamination, we use a
-- dedicated table.
CREATE TABLE admin_sessions (
    id              UUID PRIMARY KEY,
    admin_id        UUID NOT NULL REFERENCES platform_admins(id) ON DELETE CASCADE,
    token_hash      BYTEA NOT NULL UNIQUE,
    user_agent      TEXT,
    ip              INET,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_sessions_admin ON admin_sessions (admin_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_admin_sessions_expires ON admin_sessions (expires_at) WHERE revoked_at IS NULL;

-- Admin audit log. Every action performed in the admin panel gets a row
-- here. Separate table from audit_logs so we can hand out admin access
-- without exposing tenant data or vice versa.
CREATE TABLE admin_audit_logs (
    id              UUID PRIMARY KEY,
    admin_id        UUID REFERENCES platform_admins(id),
    action          TEXT NOT NULL,          -- e.g. "org.suspended", "user.deleted"
    target_type     TEXT,                   -- "ORGANIZATION", "USER", "ORDER", etc.
    target_id       UUID,
    metadata        JSONB,
    ip              INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_audit_admin ON admin_audit_logs (admin_id, created_at DESC);
CREATE INDEX idx_admin_audit_target ON admin_audit_logs (target_type, target_id, created_at DESC);
CREATE INDEX idx_admin_audit_recent ON admin_audit_logs (created_at DESC);
