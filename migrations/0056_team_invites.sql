-- =============================================================
-- 0056_team_invites.sql
--
-- Dead-simple team invites: OWNER can invite up to 2 extra people
-- (max 3 total per org: 1 owner + 2 staff). No email sending —
-- owner copies the /join/{token} link and shares it manually.
--
-- Table is tenant-scoped (RLS) like organization_members. Accept
-- flow is pre-auth (no tenant yet), so the lookup by token hash
-- goes through a SECURITY DEFINER function, same pattern as
-- list_user_memberships (0005).
-- =============================================================

CREATE TABLE organization_invites (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           CITEXT NOT NULL,
    token_hash      BYTEA NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING','ACCEPTED','REVOKED','EXPIRED')),
    invited_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    accepted_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, email)
);
CREATE INDEX idx_org_invites_org_status
    ON organization_invites(organization_id, status);

-- Tenant scoping.
ALTER TABLE organization_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_invites FORCE ROW LEVEL SECURITY;
CREATE POLICY organization_invites_tenant ON organization_invites
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- Pre-auth lookup by token hash. Runs as the table owner with RLS
-- off (FORCE RLS applies to owners too). Only ordora_app may call it.
CREATE OR REPLACE FUNCTION get_team_invite_by_token(p_token_hash bytea)
RETURNS TABLE (
    invite_id       uuid,
    organization_id uuid,
    organization_name text,
    email           text,
    status          text,
    expires_at      timestamptz,
    created_at      timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    RETURN QUERY
        SELECT i.id, i.organization_id, o.name, i.email::text,
               i.status, i.expires_at, i.created_at
        FROM organization_invites i
        JOIN organizations o ON o.id = i.organization_id
        WHERE i.token_hash = p_token_hash;
END;
$$;

REVOKE ALL ON FUNCTION get_team_invite_by_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_team_invite_by_token(bytea) TO ordora_app;
