-- =============================================================
-- 0049_asoebi_public_reads.sql
--
-- Public join/manage pages run with no login (no tenant scope), so
-- direct SELECTs on RLS-FORCE tables return zero rows. All public
-- reads must go through SECURITY DEFINER functions.
-- Adds list_group_members for the bride manage table.
-- =============================================================

-- lookup_group_join must also hand out the org id so the public form
-- can load the measurement template tenant-scoped. Redefined here
-- (drop + recreate: return type changes).
DROP FUNCTION IF EXISTS lookup_group_join(TEXT);

CREATE OR REPLACE FUNCTION lookup_group_join(p_slug TEXT)
RETURNS TABLE (
    group_id uuid,
    org_id uuid,
    org_name text,
    group_name text,
    fabric text,
    occasion_date date,
    notes text,
    price_minor bigint,
    currency char(3),
    template_id uuid,
    join_enabled boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT g.id, g.organization_id, o.name, g.name, g.fabric, g.occasion_date,
               g.notes, g.price_minor, g.currency,
               g.measurement_template_id, g.join_enabled
        FROM order_groups g
        JOIN organizations o ON o.id = g.organization_id
        WHERE g.join_slug = p_slug
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_group_join(TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_group_join(TEXT) TO ordora_app;

-- lookup_group_manage also returns the public slug for the path guard.
DROP FUNCTION IF EXISTS lookup_group_manage(BYTEA);

CREATE OR REPLACE FUNCTION lookup_group_manage(p_manage_hash BYTEA)
RETURNS TABLE (
    group_id uuid,
    org_name text,
    group_name text,
    fabric text,
    occasion_date date,
    price_minor bigint,
    currency char(3),
    join_slug text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT g.id, o.name, g.name, g.fabric, g.occasion_date,
               g.price_minor, g.currency, g.join_slug
        FROM order_groups g
        JOIN organizations o ON o.id = g.organization_id
        WHERE g.manage_token_hash = p_manage_hash
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_group_manage(BYTEA) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_group_manage(BYTEA) TO ordora_app;

CREATE OR REPLACE FUNCTION list_group_members(p_manage_hash BYTEA)
RETURNS TABLE (
    order_id uuid,
    order_number text,
    customer_id uuid,
    customer_name text,
    customer_phone text,
    title text,
    status text,
    currency char(3),
    total_minor bigint,
    paid_minor bigint,
    member_paid boolean,
    collected boolean,
    expected_completion date
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_group_id UUID;
BEGIN
    SELECT id INTO v_group_id FROM order_groups
     WHERE manage_token_hash = p_manage_hash LIMIT 1;
    IF v_group_id IS NULL THEN RETURN; END IF;

    RETURN QUERY
        SELECT o.id, o.order_number, o.customer_id, c.name, COALESCE(c.phone,''),
               o.title, o.status::text, o.currency, o.total_minor, o.amount_paid_minor,
               o.member_paid, o.collected, o.expected_completion
        FROM orders o
        JOIN customers c ON c.id = o.customer_id
        WHERE o.group_id = v_group_id
        ORDER BY c.name ASC, o.created_at ASC;
END;
$$;

REVOKE ALL ON FUNCTION list_group_members(BYTEA) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION list_group_members(BYTEA) TO ordora_app;
