-- =============================================================
-- 0038_public_lookup_ambiguous.sql
--
-- Fixes lookup_public_org: ERROR column reference "currency" is
-- ambiguous (SQLSTATE 42702).
--
-- The RETURNS TABLE out-params (currency, email, slug) collide with
-- the unqualified column names in the function body, so PL/pgSQL
-- refuses to run it — every public intake link failed with
-- "could not load business". Qualifying the columns with a table
-- alias removes the ambiguity.
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
        SELECT o.id, o.name, o.currency,
               COALESCE(o.email::text, ''), o.slug::text
        FROM organizations o
        WHERE o.slug = p_slug
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_public_org(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_public_org(citext) TO ordora_app;
