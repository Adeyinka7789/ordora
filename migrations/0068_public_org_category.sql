-- =============================================================
-- 0068_public_org_category.sql
--
-- The public storefront adapts its vocabulary to the business
-- (packages + event types for photographers, services for salons,
-- ...). lookup_public_org must therefore return the org's
-- business_category alongside the existing columns. Read-time
-- derivation only: no stored mode, no backfill — blank stays
-- blank and reads as the generic storefront.
-- =============================================================

DROP FUNCTION IF EXISTS lookup_public_org(citext);

CREATE OR REPLACE FUNCTION lookup_public_org(p_slug citext)
RETURNS TABLE (
    org_id            uuid,
    org_name          text,
    currency          char(3),
    email             text,
    slug              text,
    business_category text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT o.id, o.name, o.currency,
               COALESCE(o.email::text, ''), o.slug::text,
               COALESCE(o.business_category, '')
        FROM organizations o
        WHERE o.slug = p_slug
          AND o.suspended_at IS NULL
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_public_org(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_public_org(citext) TO ordora_app;
