-- =============================================================
-- 0066_tracking_lookup.sql
--
-- Long-lived customer tracking links for emails (/o/o1_...).
--
-- Random portal bearer tokens cannot back email links: the raw bearer
-- exists only at mint time (creation discards it, regeneration shows it
-- once), so status-change/payment/reschedule emails can never rebuild
-- the URL. Tracking links are instead HMAC-signed order ids, verified in
-- Go (app.VerifyTrackingToken) — stable forever, derivable anywhere.
--
-- This function resolves an already-verified order id to the same portal
-- projection as get_order_by_public_token, so the portal renders both
-- token kinds identically.
--
-- Security properties (same as 0014/0018/0032):
--   - SECURITY DEFINER, SET search_path = public
--   - Only granted to ordora_app
--   - No listing: takes one order id the caller already authorized via
--     HMAC; returns rows for that order only.
--   - Bearer-equivalent: the link grants read access to that one order.
--     Unlike shareable portal tokens it has no revocation — rotating the
--     portal token does NOT kill email links (old emails keep working).
-- =============================================================

CREATE OR REPLACE FUNCTION get_order_by_id_public(p_order_id uuid)
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
BEGIN
    UPDATE orders
    SET public_token_last_accessed_at = now()
    WHERE id = p_order_id;

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
        WHERE o.id = p_order_id;
END;
$$;

REVOKE ALL ON FUNCTION get_order_by_id_public(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_order_by_id_public(uuid) TO ordora_app;
