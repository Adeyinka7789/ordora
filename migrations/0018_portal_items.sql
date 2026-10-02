-- =============================================================
-- 0018_portal_items.sql
--
-- Returns the line items for one order, called by the portal
-- handler after it has validated the token. Narrowly scoped:
-- takes an order id, returns its items.
-- =============================================================

CREATE OR REPLACE FUNCTION get_public_order_items(p_order_id uuid)
RETURNS TABLE (
    description      text,
    quantity         numeric,
    unit_price_minor bigint,
    subtotal_minor   bigint,
    currency         text,
    position         int
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT
            oi.description,
            oi.quantity,
            oi.unit_price_minor,
            oi.subtotal_minor,
            o.currency::text,
            oi.position
        FROM order_items oi
        JOIN orders o ON o.id = oi.order_id
        WHERE oi.order_id = p_order_id
        ORDER BY oi.position ASC, oi.id ASC;
END;
$$;

REVOKE ALL ON FUNCTION get_public_order_items(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_order_items(uuid) TO ordora_app;
