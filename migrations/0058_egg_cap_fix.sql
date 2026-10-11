-- =============================================================
-- 0058_egg_cap_fix.sql
--
-- consume_crate_cap (0057) never executed successfully: its OUT
-- parameters (sold_after, cap, ...) collide with the CTE column
-- names in the final SELECT, so Postgres raises "column reference
-- is ambiguous" (42702) on every call. Same signature and grants —
-- only the inner references are qualified now.
-- =============================================================

CREATE OR REPLACE FUNCTION consume_crate_cap(
    p_slug citext,
    p_day  date,
    p_qty  numeric
)
RETURNS TABLE (
    sold_after numeric,
    cap        integer,
    accepting  boolean,
    org_id     uuid
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_org_id uuid;
BEGIN
    SET LOCAL row_security = off;

    SELECT o.id INTO v_org_id
      FROM organizations o
     WHERE o.slug = p_slug
       AND o.suspended_at IS NULL
     LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    RETURN QUERY
        WITH s AS (
            SELECT COALESCE(ps.accepting_orders, TRUE) AS accepting,
                   COALESCE(ps.max_crates_per_day, 100) AS cap
              FROM (SELECT 1) AS one
              LEFT JOIN poultry_settings ps ON ps.organization_id = v_org_id
        ),
        up AS (
            INSERT INTO daily_crate_counters (organization_id, day, sold, updated_at)
            VALUES (v_org_id, p_day, p_qty, now())
            ON CONFLICT (organization_id, day)
            DO UPDATE SET sold = daily_crate_counters.sold + EXCLUDED.sold,
                          updated_at = now()
            RETURNING daily_crate_counters.sold AS sold
        )
        SELECT (SELECT up.sold FROM up),
               (SELECT s.cap FROM s),
               (SELECT s.accepting FROM s),
               v_org_id;
END;
$$;

REVOKE ALL ON FUNCTION consume_crate_cap(citext, date, numeric) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION consume_crate_cap(citext, date, numeric) TO ordora_app;
