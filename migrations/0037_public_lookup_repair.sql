-- =============================================================
-- 0037_public_lookup_repair.sql
--
-- Self-healing re-creation of lookup_public_org.
--
-- The intake handler scans five columns (id, name, currency, email,
-- slug). Databases migrated before 0020 still carry the three-column
-- version, which makes every public intake link fail with
-- "could not load business". Re-creating the function here is a safe
-- no-op for up-to-date databases and repairs drifted ones.
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
