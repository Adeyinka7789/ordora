-- =============================================================
-- 0065_booking_track_product.sql
--
-- The tracking page's "Book again" button needs the booked service:
-- get_booking_by_token gains the booking's product id (nullable —
-- older rows always set it, but the column stays honest). Shape
-- grows, so DROP first (Postgres forbids shape changes under
-- CREATE OR REPLACE, 42P13 — same convention as 0041/0055).
-- =============================================================

DROP FUNCTION IF EXISTS get_booking_by_token(bytea);

CREATE OR REPLACE FUNCTION get_booking_by_token(p_token_hash bytea)
RETURNS TABLE (
    start_at     timestamptz,
    end_at       timestamptz,
    timezone     text,
    status       text,
    service_name text,
    org_name     text,
    product_id   uuid
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    RETURN QUERY
        SELECT b.start_at, b.end_at, b.timezone, b.status,
               COALESCE(p.name, ''), o2.name, b.product_id
          FROM bookings b
          JOIN orders o ON o.id = b.order_id
          JOIN organizations o2 ON o2.id = b.organization_id
          LEFT JOIN products p ON p.id = b.product_id
         WHERE o.public_token_hash = p_token_hash
           AND o.public_token_revoked_at IS NULL
         LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_by_token(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_by_token(bytea) TO ordora_app;
