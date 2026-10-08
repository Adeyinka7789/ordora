-- =============================================================
-- 0055_public_catalog_details.sql
--
-- Product detail modal on the public intake form needs specs, color,
-- production lead time, and the full gallery (not just the cover).
-- Same signature family as 0052/0054 — shape grows, so DROP first
-- (Postgres forbids shape changes under CREATE OR REPLACE, 42P13).
-- =============================================================

DROP FUNCTION IF EXISTS get_public_products(citext);

CREATE OR REPLACE FUNCTION get_public_products(p_slug citext)
RETURNS TABLE (
    product_id          uuid,
    product_name        text,
    product_description text,
    product_short       text,
    product_material    text,
    product_category    text,
    unit_price_minor    bigint,
    currency            text,
    quote_only          boolean,
    starting_from       boolean,
    availability        text,
    cover_image_id      uuid,
    product_questions   jsonb,
    product_specs       text,
    product_color       text,
    product_production_days int,
    product_image_ids   text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT p.id, p.name,
               COALESCE(p.description, ''),
               COALESCE(p.short_description, ''),
               COALESCE(p.material, ''),
               COALESCE(p.category, ''),
               p.unit_price_minor, p.currency::text,
               p.quote_only, p.starting_from, p.availability,
               (SELECT a.id FROM attachments a
                 WHERE a.entity_type = 'PRODUCT'
                   AND a.entity_id = p.id
                   AND a.purpose = 'product_cover'
                 ORDER BY a.created_at DESC
                 LIMIT 1),
               COALESCE(p.questions, '[]'),
               COALESCE(p.specs, ''),
               COALESCE(p.color, ''),
               COALESCE(p.production_days, 0),
               COALESCE((
                   SELECT string_agg(a.id::text, ',' ORDER BY (a.purpose = 'product_cover') DESC, a.created_at DESC)
                     FROM attachments a
                    WHERE a.entity_type = 'PRODUCT'
                      AND a.entity_id = p.id
                      AND a.purpose IN ('product_gallery', 'product_cover')
               ), '')
        FROM products p
        JOIN organizations o ON o.id = p.organization_id
        WHERE o.slug = p_slug
          AND o.suspended_at IS NULL
          AND p.active
          AND NOT p.hidden
          AND p.availability <> 'out_of_stock'
        ORDER BY p.name ASC
        LIMIT 50;
END;
$$;

REVOKE ALL ON FUNCTION get_public_products(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_products(citext) TO ordora_app;
