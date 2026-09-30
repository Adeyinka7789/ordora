-- =============================================================
-- 0005_membership_list_function.sql
--
-- Allows the app to read a user's memberships before knowing
-- which tenant to set. Needed by the login flow.
--
-- SECURITY DEFINER runs the function as its owner (ordora_admin),
-- which owns organization_members. However, FORCE ROW LEVEL
-- SECURITY applies to owners too, so we must additionally use
-- row_security = off inside the function.
--
-- ordora_admin is NOT a superuser and does NOT have BYPASSRLS.
-- The only role granted EXECUTE is ordora_app.
-- =============================================================

CREATE OR REPLACE FUNCTION list_user_memberships(p_user_id uuid)
RETURNS TABLE (
    organization_id uuid,
    organization_name text,
    organization_slug text,
    role text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    -- We are the table owner; turn off RLS for this function body only.
    -- Requires that the function owner is the table owner, which it is.
    SET LOCAL row_security = off;

    RETURN QUERY
        SELECT om.organization_id, o.name, o.slug::text, om.role
        FROM organization_members om
        JOIN organizations o ON o.id = om.organization_id
        WHERE om.user_id = p_user_id
          AND om.status = 'ACTIVE'
        ORDER BY om.created_at ASC;
END;
$$;

REVOKE ALL ON FUNCTION list_user_memberships(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION list_user_memberships(uuid) TO ordora_app;