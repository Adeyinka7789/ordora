-- =============================================================
-- 0016_portal_function_text_casts.sql
--
-- Recreates get_order_by_public_token with explicit ::text casts
-- on citext and char(3) columns. pgx doesn't scan those types
-- cleanly without extra registration.
-- =============================================================

DROP FUNCTION IF EXISTS get_order_by_public_token(bytea);

CREATE OR REPLACE FUNCTION get_order_by_public_token(p_token_hash bytea)
RETURNS TABLE (
    order_id              uuid,
    organization_id       uuid,
    order_number          text,
    title                 text,
    description           text,
    status                text,
    currency              text,
    subtotal_minor        bigint,
    discount_minor        bigint,
    tax_minor             bigint,
    total_minor           bigint,
    amount_paid_minor     bigint,
    expected_completion   date,
    delivered_at          timestamptz,
    created_at            timestamptz,
    customer_name         text,
    customer_email        text,
    customer_phone        text,
    org_name              text,
    org_slug              text,
    org_email             text,
    org_phone             text,
    org_address           text,
    org_currency          text,
    org_timezone          text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_order_id uuid;
BEGIN
    SELECT id INTO v_order_id
    FROM orders
    WHERE public_token_hash = p_token_hash
      AND public_token_revoked_at IS NULL
    LIMIT 1;

    IF v_order_id IS NULL THEN
        RETURN;
    END IF;

    UPDATE orders
    SET public_token_last_accessed_at = now()
    WHERE id = v_order_id;

    RETURN QUERY
        SELECT
            o.id, o.organization_id, o.order_number, o.title,
            COALESCE(o.description, ''),
            o.status::text, o.currency::text,
            o.subtotal_minor, o.discount_minor, o.tax_minor,
            o.total_minor, o.amount_paid_minor,
            o.expected_completion, o.delivered_at, o.created_at,
            c.name, COALESCE(c.email::text, ''), COALESCE(c.phone, ''),
            org.name, org.slug::text, org.email::text,
            COALESCE(org.phone, ''), COALESCE(org.address, ''),
            org.currency::text, org.timezone
        FROM orders o
        JOIN customers c      ON c.id = o.customer_id
        JOIN organizations org ON org.id = o.organization_id
        WHERE o.id = v_order_id;
END;
$$;

REVOKE ALL ON FUNCTION get_order_by_public_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_order_by_public_token(bytea) TO ordora_app;
