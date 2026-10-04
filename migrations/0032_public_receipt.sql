-- =============================================================
-- 0032_public_receipt.sql
--
-- Receipt support for the public customer portal.
--
-- 1. Extends get_order_by_public_token with the customer's address
--    (needed on the OPay-style receipt: customer name + address).
--    CREATE OR REPLACE keeps the same name/signature shape; the new
--    column is appended at the end of the RETURNS TABLE list.
--
-- 2. Adds get_public_order_payments(order_id): per-payment lines for
--    the receipt (method, reference, paid_at, amount, notes, reversal
--    flags) plus proof-attachment metadata. Only filenames/counts are
--    exposed — file downloads stay behind staff auth (/attachments/{id}).
--
-- Security properties (same as 0014/0018):
--   - SECURITY DEFINER, SET search_path = public
--   - Only granted to ordora_app
--   - No listing: payments function takes an order id the caller
--     already validated via token; it returns rows for that order only.
-- =============================================================

-- 1. Customer address on the portal projection.
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
    customer_address      text,
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
            o.status::text, o.currency,
            o.subtotal_minor, o.discount_minor, o.tax_minor,
            o.total_minor, o.amount_paid_minor,
            o.expected_completion, o.delivered_at, o.created_at,
            c.name, COALESCE(c.email::text, ''), COALESCE(c.phone, ''),
            COALESCE(c.address, ''),
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

-- 2. Per-payment lines for the public receipt.
DROP FUNCTION IF EXISTS get_public_order_payments(uuid);

CREATE OR REPLACE FUNCTION get_public_order_payments(p_order_id uuid)
RETURNS TABLE (
    method          text,
    reference       text,
    paid_at         timestamptz,
    amount_minor    bigint,
    currency        char(3),
    notes           text,
    is_reversed     boolean,
    is_reversal     boolean,
    proof_count     int,
    proof_names     text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT
            p.method::text,
            COALESCE(p.reference, ''),
            p.paid_at,
            p.amount_minor,
            p.currency,
            COALESCE(p.notes, ''),
            (p.reversed_by IS NOT NULL),
            (p.reverses IS NOT NULL),
            COUNT(a.id)::int,
            COALESCE(string_agg(a.filename, ', ' ORDER BY a.created_at), '')
        FROM payments p
        LEFT JOIN attachments a
            ON a.entity_type = 'PAYMENT'
            AND a.entity_id = p.id
        WHERE p.order_id = p_order_id
        GROUP BY p.id
        ORDER BY p.paid_at DESC, p.created_at DESC;
END;
$$;

REVOKE ALL ON FUNCTION get_public_order_payments(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_order_payments(uuid) TO ordora_app;
