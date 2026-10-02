-- =============================================================
-- 0014_portal_function.sql
--
-- SECURITY DEFINER function that lets the public portal read a
-- single order by its token hash, without authentication and
-- without disabling RLS for the app role.
--
-- Called with:  get_order_by_public_token(token_hash bytea)
-- Returns:      one row with the order + business + customer fields
--               needed by the customer portal.
--
-- Security properties:
--   - Only granted to ordora_app
--   - Returns at most one row (token is unique)
--   - Cannot list orders
--   - Bumps public_token_last_accessed_at on success
--   - Refuses revoked tokens
-- =============================================================

CREATE OR REPLACE FUNCTION get_order_by_public_token(p_token_hash bytea)
RETURNS TABLE (
    order_id              uuid,
    organization_id       uuid,
    order_number          text,
    title                 text,
    description           text,
    status                text,
    currency              char(3),
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
    org_slug              citext,
    org_email             citext,
    org_phone             text,
    org_address           text,
    org_currency          char(3),
    org_timezone          text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_order_id uuid;
BEGIN
    -- Resolve the token to an order id (single row expected).
    SELECT id INTO v_order_id
    FROM orders
    WHERE public_token_hash = p_token_hash
      AND public_token_revoked_at IS NULL
    LIMIT 1;

    IF v_order_id IS NULL THEN
        RETURN;
    END IF;

    -- Bump last_accessed_at (best-effort, doesn't block).
    UPDATE orders
    SET public_token_last_accessed_at = now()
    WHERE id = v_order_id;

    -- Return the projection.
    RETURN QUERY
        SELECT
            o.id, o.organization_id, o.order_number, o.title,
            COALESCE(o.description, ''),
            o.status::text, o.currency,
            o.subtotal_minor, o.discount_minor, o.tax_minor,
            o.total_minor, o.amount_paid_minor,
            o.expected_completion, o.delivered_at, o.created_at,
            c.name, COALESCE(c.email::text, ''), COALESCE(c.phone, ''),
            org.name, org.slug, org.email, COALESCE(org.phone, ''),
            COALESCE(org.address, ''), org.currency, org.timezone
        FROM orders o
        JOIN customers c      ON c.id = o.customer_id
        JOIN organizations org ON org.id = o.organization_id
        WHERE o.id = v_order_id;
END;
$$;

REVOKE ALL ON FUNCTION get_order_by_public_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_order_by_public_token(bytea) TO ordora_app;
