-- =============================================================
-- 0020_public_org_email.sql
--
-- Extends lookup_public_org to also return the org email and slug,
-- so the intake form can notify the owner and build an admin URL.
-- =============================================================

DROP FUNCTION IF EXISTS lookup_public_org(citext);

CREATE OR REPLACE FUNCTION lookup_public_org(p_slug citext)
RETURNS TABLE (
    org_id   uuid,
    org_name text,
    currency char(3),
    email    text,
    slug     text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT id, name, currency, COALESCE(email::text, ''), slug::text
        FROM organizations
        WHERE slug = p_slug
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_public_org(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_public_org(citext) TO ordora_app;
