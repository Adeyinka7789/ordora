-- =============================================================
-- 0060_booking_config_fix.sql
--
-- get_booking_config (0059) never executed successfully: its final
-- SELECT references the booking_settings alias without joining the
-- table ("missing FROM-clause entry for table s", 42P01) on every
-- call. Same signature and grants — the settings join is added.
-- =============================================================

CREATE OR REPLACE FUNCTION get_booking_config(p_slug citext)
RETURNS TABLE (
    org_id             uuid,
    eligible           boolean,
    flag_on            boolean,
    accepting          boolean,
    duration_min       integer,
    buf_before         integer,
    buf_after          integer,
    min_notice_hours   integer,
    max_advance_days   integer,
    cancel_policy      text,
    org_timezone       text,
    currency           text,
    org_name           text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_org_id   uuid;
    v_category text;
    v_timezone text;
    v_currency text;
    v_name     text;
    v_flag     boolean := FALSE;
    v_ov       boolean;
BEGIN
    SET LOCAL row_security = off;

    SELECT o.id, COALESCE(o.business_category, ''), o.timezone,
           o.currency::text, o.name
      INTO v_org_id, v_category, v_timezone, v_currency, v_name
      FROM organizations o
     WHERE o.slug = p_slug
       AND o.suspended_at IS NULL
     LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Pilot gate: per-org override wins, else the flag must be fully on.
    SELECT ov.enabled INTO v_ov
      FROM feature_flag_overrides ov
     WHERE ov.flag_key = 'booking_module' AND ov.organization_id = v_org_id;
    IF FOUND THEN
        v_flag := v_ov;
    ELSE
        SELECT f.enabled AND f.rollout_percent >= 100 INTO v_flag
          FROM feature_flags f WHERE f.flag_key = 'booking_module';
        v_flag := COALESCE(v_flag, FALSE);
    END IF;

    RETURN QUERY
        SELECT v_org_id,
               (v_category = 'Beauty & Salon'),
               v_flag,
               COALESCE(s.accepting_bookings, TRUE),
               COALESCE(s.default_duration_min, 60),
               COALESCE(s.buffer_before_min, 0),
               COALESCE(s.buffer_after_min, 0),
               COALESCE(s.min_notice_hours, 2),
               COALESCE(s.max_advance_days, 30),
               COALESCE(s.cancellation_policy, ''),
               v_timezone,
               v_currency,
               v_name
          FROM (SELECT 1) AS one
          LEFT JOIN booking_settings s ON s.organization_id = v_org_id;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_config(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_config(citext) TO ordora_app;
