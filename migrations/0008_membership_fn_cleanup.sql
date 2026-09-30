-- =============================================================
-- 0008_membership_fn_cleanup.sql
--
-- Redefines list_user_memberships without the misleading
-- SET LOCAL row_security = off line. The bypass is now provided
-- by the ordora_admin role's BYPASSRLS attribute, which is the
-- only mechanism that actually works with FORCE ROW LEVEL SECURITY.
--
-- Reminder: BYPASSRLS cannot be granted by ordora_admin to itself
-- (only a superuser can). See migration 0007 notes for how it was
-- applied.
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