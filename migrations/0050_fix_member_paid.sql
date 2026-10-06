-- =============================================================
-- 0050_fix_member_paid.sql
--
-- set_group_member_paid used GET DIAGNOSTICS ... ROW_COUNT into a
-- BOOLEAN variable, which raises a runtime error on every call, so
-- the bride's Mark paid button always failed with "Could not update."
-- ROW_COUNT needs an integer target.
-- =============================================================

CREATE OR REPLACE FUNCTION set_group_member_paid(
    p_manage_hash BYTEA,
    p_order_id    UUID,
    p_paid        BOOLEAN
)
RETURNS BOOLEAN
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_group_id UUID;
    v_rows     BIGINT := 0;
BEGIN
    SELECT id INTO v_group_id FROM order_groups
     WHERE manage_token_hash = p_manage_hash LIMIT 1;
    IF v_group_id IS NULL THEN RETURN FALSE; END IF;

    UPDATE orders
       SET member_paid = p_paid,
           member_paid_at = CASE WHEN p_paid THEN now() ELSE NULL END,
           updated_at = now()
     WHERE id = p_order_id AND group_id = v_group_id;

    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows > 0;
END;
$$;

REVOKE ALL ON FUNCTION set_group_member_paid(BYTEA, UUID, BOOLEAN) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION set_group_member_paid(BYTEA, UUID, BOOLEAN) TO ordora_app;
