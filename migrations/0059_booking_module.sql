-- =============================================================
-- 0059_booking_module.sql
--
-- Appointment booking for service businesses (salons first):
-- customers pick a service + slot on a public link, staff manage
-- a day calendar. The order stays the commercial record; bookings
-- is the time record linked 1:1 to it.
--
-- Tables are tenant-scoped (RLS) like everything else. The two
-- public functions are SECURITY DEFINER with row_security off
-- (pattern of 0005/0056/0057); only ordora_app may call them.
--
-- Overlap safety (deliberate, no exclusion constraint): there is
-- no btree_gist dependency on managed Postgres boxes, so
-- create_booking takes SELECT ... FOR UPDATE on the parent
-- organizations row first, then re-checks overlap. Concurrent
-- submissions for one org serialize on that lock; different orgs
-- never contend. Cancelled/no-show rows never block (checked by
-- status), and every check below re-runs inside the same tx, so a
-- form rendered minutes ago cannot sneak past changed hours.
-- =============================================================

-- Kill switch + pilot gate. Seeded OFF: enable per-org via
-- feature_flag_overrides for the pilot, flip on for rollout.
INSERT INTO feature_flags(flag_key, name, description, enabled, rollout_percent)
VALUES ('booking_module', 'Booking module',
        'Appointment booking for salons and service businesses',
        FALSE, 0)
ON CONFLICT DO NOTHING;

-- -------------------------------------------------------------
-- Per-org booking settings (one row, created on first save).
-- -------------------------------------------------------------
CREATE TABLE booking_settings (
    organization_id      UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    accepting_bookings   BOOLEAN NOT NULL DEFAULT TRUE,
    default_duration_min INT NOT NULL DEFAULT 60 CHECK (default_duration_min BETWEEN 15 AND 480),
    buffer_before_min    INT NOT NULL DEFAULT 0 CHECK (buffer_before_min BETWEEN 0 AND 120),
    buffer_after_min     INT NOT NULL DEFAULT 0 CHECK (buffer_after_min BETWEEN 0 AND 120),
    min_notice_hours     INT NOT NULL DEFAULT 2 CHECK (min_notice_hours BETWEEN 0 AND 720),
    max_advance_days     INT NOT NULL DEFAULT 30 CHECK (max_advance_days BETWEEN 1 AND 365),
    cancellation_policy  TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE booking_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE booking_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY booking_settings_tenant ON booking_settings
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Weekly hours. Absent rows mean "default day" (Mon-Sat 09:00-17:00,
-- Sun closed) — applied in app code, so sellers only store
-- deviations. Times are minutes since midnight, org-local.
-- -------------------------------------------------------------
CREATE TABLE booking_hours (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    dow             SMALLINT NOT NULL CHECK (dow BETWEEN 0 AND 6),
    open_min        INT NOT NULL CHECK (open_min BETWEEN 0 AND 1440),
    close_min       INT NOT NULL CHECK (close_min BETWEEN 0 AND 1440),
    is_open         BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (organization_id, dow)
);

ALTER TABLE booking_hours ENABLE ROW LEVEL SECURITY;
ALTER TABLE booking_hours FORCE ROW LEVEL SECURITY;
CREATE POLICY booking_hours_tenant ON booking_hours
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Blackout dates (holidays, closures). One row per closed day.
-- -------------------------------------------------------------
CREATE TABLE booking_blackouts (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    day             DATE NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, day)
);
CREATE INDEX idx_booking_blackouts_org_day
    ON booking_blackouts(organization_id, day);

ALTER TABLE booking_blackouts ENABLE ROW LEVEL SECURITY;
ALTER TABLE booking_blackouts FORCE ROW LEVEL SECURITY;
CREATE POLICY booking_blackouts_tenant ON booking_blackouts
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Appointments. start_at/end_at are UTC instants; timezone keeps
-- the org-local name for display. Buffers snapshot the settings
-- at submit time so later config edits can't rewrite history.
-- order_id is UNIQUE: one commercial record per visit, and staff
-- orders never become bookings (no row, no link).
-- -------------------------------------------------------------
CREATE TABLE bookings (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    order_id        UUID NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    product_id      UUID REFERENCES products(id) ON DELETE SET NULL,
    staff_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    start_at        TIMESTAMPTZ NOT NULL,
    end_at          TIMESTAMPTZ NOT NULL CHECK (end_at > start_at),
    timezone        TEXT NOT NULL DEFAULT 'Africa/Lagos',
    buf_before_min  INT NOT NULL DEFAULT 0,
    buf_after_min   INT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'REQUESTED'
                    CHECK (status IN ('REQUESTED','CONFIRMED','COMPLETED','CANCELLED','NO_SHOW')),
    customer_notes  TEXT NOT NULL DEFAULT '',
    cancel_reason   TEXT NOT NULL DEFAULT '',
    rescheduled_at  TIMESTAMPTZ,
    prev_start_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bookings_org_start ON bookings(organization_id, start_at);
CREATE INDEX idx_bookings_org_status ON bookings(organization_id, status);
CREATE INDEX idx_bookings_order ON bookings(order_id);

ALTER TABLE bookings ENABLE ROW LEVEL SECURITY;
ALTER TABLE bookings FORCE ROW LEVEL SECURITY;
CREATE POLICY bookings_tenant ON bookings
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Anonymous booking config for one slug: gate, settings, hours.
-- Suspended/unknown slugs return no rows (caller 404s).
-- -------------------------------------------------------------
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

-- -------------------------------------------------------------
-- Weekly hours + blackouts for the public slot picker. Suspended/
-- unknown slugs yield no rows.
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION get_booking_hours(p_slug citext)
RETURNS TABLE (
    dow       smallint,
    open_min  integer,
    close_min integer,
    is_open   boolean
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
        SELECT h.dow, h.open_min, h.close_min, h.is_open
          FROM booking_hours h
         WHERE h.organization_id = v_org_id
         ORDER BY h.dow;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_hours(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_hours(citext) TO ordora_app;

CREATE OR REPLACE FUNCTION get_booking_blackouts(p_slug citext, p_from date, p_to date)
RETURNS TABLE (
    day    date,
    reason text
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
        SELECT b.day, b.reason
          FROM booking_blackouts b
         WHERE b.organization_id = v_org_id
           AND b.day BETWEEN p_from AND p_to
         ORDER BY b.day;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_blackouts(citext, date, date) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_blackouts(citext, date, date) TO ordora_app;

-- -------------------------------------------------------------
-- Live bookings overlapping a UTC window (for the slot picker).
-- Only REQUESTED/CONFIRMED block; cancelled/no-show/completed don't.
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION get_booking_busy(p_slug citext, p_from timestamptz, p_to timestamptz)
RETURNS TABLE (
    start_at   timestamptz,
    end_at     timestamptz,
    buf_before integer,
    buf_after  integer
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
        SELECT b.start_at, b.end_at, b.buf_before_min, b.buf_after_min
          FROM bookings b
         WHERE b.organization_id = v_org_id
           AND b.status IN ('REQUESTED', 'CONFIRMED')
           AND tstzrange(b.start_at, b.end_at, '[)') &&
               tstzrange(p_from, p_to, '[)')
         ORDER BY b.start_at;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_busy(citext, timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_busy(citext, timestamptz, timestamptz) TO ordora_app;

-- -------------------------------------------------------------
-- Atomic booking creation. One transaction: org lock, eligibility,
-- settings/notice/window/hours/blackout re-validation, overlap
-- re-check, then customer + order + item (via create_public_order)
-- + booking row + public token. Anything failing rolls back all
-- of it — no orphaned orders, counters, or notifications.
--
-- o_error is '' on success, else one of:
--   NOT_ELIGIBLE, NOT_ACCEPTING, BAD_SLOT, SLOT_TAKEN
-- (Go maps these; no RAISE-message parsing.)
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION create_booking(
    p_slug         citext,
    p_cust_name    text,
    p_cust_email   citext,
    p_cust_phone   text,
    p_notes        text,
    p_service_name text,
    p_product_id   uuid,
    p_start        timestamptz,
    p_end          timestamptz,
    p_tz           text,
    p_buf_before   integer,
    p_buf_after    integer,
    p_token_hash   bytea,
    p_order_id     uuid,
    p_customer_id  uuid,
    p_audit_id     uuid,
    p_booking_id   uuid
)
RETURNS TABLE (
    o_error      text,
    o_order_id   uuid,
    o_order_num  text,
    o_total      bigint,
    o_currency   text,
    o_org_email  text,
    o_booking_id uuid
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_org_id    uuid;
    v_category  text;
    v_currency  text;
    v_flag      boolean := FALSE;
    v_ov        boolean;
    v_accepting boolean := TRUE;
    v_notice    integer := 2;
    v_advance   integer := 30;
    v_open      integer;
    v_close     integer;
    v_is_open   boolean;
    v_dow       integer;
    v_day       date;
    v_open_t    timestamptz;
    v_close_t   timestamptz;
    r_order     record;
    v_items     jsonb;
    v_cust_id   uuid;
BEGIN
    SET LOCAL row_security = off;

    SELECT o.id, COALESCE(o.business_category, ''), o.currency::text
      INTO v_org_id, v_category, v_currency
      FROM organizations o
     WHERE o.slug = p_slug
       AND o.suspended_at IS NULL
     LIMIT 1;
    IF NOT FOUND THEN
        o_error := 'NOT_ELIGIBLE'; RETURN NEXT; RETURN;
    END IF;

    -- Serialize concurrent bookings for this org for the whole tx.
    PERFORM 1 FROM organizations WHERE id = v_org_id FOR UPDATE;

    -- Eligibility: category + pilot flag, same rule as the config fn.
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
    IF v_category <> 'Beauty & Salon' OR NOT v_flag THEN
        o_error := 'NOT_ELIGIBLE'; RETURN NEXT; RETURN;
    END IF;

    -- Settings (defaults when never saved).
    SELECT COALESCE(s.accepting_bookings, TRUE),
           COALESCE(s.min_notice_hours, 2),
           COALESCE(s.max_advance_days, 30)
      INTO v_accepting, v_notice, v_advance
      FROM (SELECT 1) AS one
      LEFT JOIN booking_settings s ON s.organization_id = v_org_id;
    IF NOT v_accepting THEN
        o_error := 'NOT_ACCEPTING'; RETURN NEXT; RETURN;
    END IF;

    -- Sanity + notice + advance window, all in absolute time.
    IF p_end <= p_start OR p_start <= now() OR
       p_start < now() + make_interval(hours => v_notice) OR
       p_start > now() + make_interval(days => v_advance) THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;

    -- Weekly hours + blackout, re-checked in the org timezone.
    v_dow := EXTRACT(DOW FROM p_start AT TIME ZONE p_tz)::integer;
    v_day := (p_start AT TIME ZONE p_tz)::date;
    SELECT h.open_min, h.close_min, h.is_open
      INTO v_open, v_close, v_is_open
      FROM booking_hours h
     WHERE h.organization_id = v_org_id AND h.dow = v_dow;
    IF NOT FOUND THEN
        -- Default week: Mon-Sat 09:00-17:00, Sun closed.
        IF v_dow = 0 THEN v_is_open := FALSE; ELSE v_is_open := TRUE; END IF;
        v_open := 540; v_close := 1020;
    END IF;
    IF NOT v_is_open THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;
    v_open_t  := (v_day + make_interval(mins => v_open))  AT TIME ZONE p_tz;
    v_close_t := (v_day + make_interval(mins => v_close)) AT TIME ZONE p_tz;
    IF p_start < v_open_t OR p_end > v_close_t THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;
    PERFORM 1 FROM booking_blackouts b
     WHERE b.organization_id = v_org_id AND b.day = v_day;
    IF FOUND THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;

    -- Overlap, with buffers on both sides. Only live bookings block.
    PERFORM 1 FROM bookings b
     WHERE b.organization_id = v_org_id
       AND b.status IN ('REQUESTED', 'CONFIRMED')
       AND tstzrange(p_start - make_interval(mins => p_buf_before),
                     p_end   + make_interval(mins => p_buf_after), '[)')
           && tstzrange(b.start_at - make_interval(mins => b.buf_before_min),
                        b.end_at   + make_interval(mins => b.buf_after_min), '[)');
    IF FOUND THEN
        o_error := 'SLOT_TAKEN'; RETURN NEXT; RETURN;
    END IF;

    -- Commercial record via the standard public intake path (customer
    -- dedup, order number, items snapshot, audit row all reused).
    IF p_product_id IS NULL THEN
        v_items := '[]';
    ELSE
        v_items := jsonb_build_array(jsonb_build_object(
            'id', gen_random_uuid(), 'product_id', p_product_id,
            'qty', '1.000'));
    END IF;
    SELECT * INTO r_order FROM create_public_order(
        p_slug,
        p_cust_name,
        NULLIF(p_cust_email, ''),
        NULLIF(p_cust_phone, ''),
        'Booking: ' || p_service_name || ' — ' ||
            to_char(p_start AT TIME ZONE p_tz, 'Dy DD Mon HH24:MI') ||
            CASE WHEN p_notes IS NULL OR p_notes = '' THEN ''
                 ELSE chr(10) || 'Notes: ' || left(p_notes, 500) END,
        (p_start AT TIME ZONE p_tz)::date,
        NULL,
        v_items,
        p_order_id, p_customer_id, p_audit_id,
        '[]'::jsonb
    );
    IF NOT FOUND THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;

    -- Public tracking token on the new order.
    UPDATE orders SET public_token_hash = p_token_hash WHERE id = p_order_id;

    -- The inner call may have matched an existing customer instead of
    -- creating one, so link through the order row (always exact).
    SELECT o.customer_id INTO v_cust_id
      FROM orders o WHERE o.id = p_order_id;
    IF NOT FOUND THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;

    INSERT INTO bookings
        (id, organization_id, order_id, customer_id, product_id,
         start_at, end_at, timezone, buf_before_min, buf_after_min,
         status, customer_notes, created_at, updated_at)
    VALUES (p_booking_id, v_org_id, p_order_id, v_cust_id, p_product_id,
            p_start, p_end, p_tz, p_buf_before, p_buf_after,
            'REQUESTED', COALESCE(left(p_notes, 2000), ''), now(), now());

    o_error := ''; o_order_id := p_order_id;
    o_order_num := r_order.order_number; o_total := r_order.total_minor;
    o_currency := r_order.org_currency; o_org_email := NULL;
    o_booking_id := p_booking_id;
    SELECT o.email::text INTO o_org_email FROM organizations o WHERE o.id = v_org_id;
    RETURN NEXT;
    RETURN;
END;
$$;

REVOKE ALL ON FUNCTION create_booking(citext, text, citext, text, text, text, uuid, timestamptz, timestamptz, text, integer, integer, bytea, uuid, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_booking(citext, text, citext, text, text, text, uuid, timestamptz, timestamptz, text, integer, integer, bytea, uuid, uuid, uuid, uuid) TO ordora_app;

-- -------------------------------------------------------------
-- Booking details for the public tracking page, resolved through
-- the order's token (same authorization posture as
-- get_portal_attachment: unguessable token, no existence signal —
-- no rows when the token is wrong or revoked).
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION get_booking_by_token(p_token_hash bytea)
RETURNS TABLE (
    start_at     timestamptz,
    end_at       timestamptz,
    timezone     text,
    status       text,
    service_name text,
    org_name     text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    RETURN QUERY
        SELECT b.start_at, b.end_at, b.timezone, b.status,
               COALESCE(p.name, ''), o2.name
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
