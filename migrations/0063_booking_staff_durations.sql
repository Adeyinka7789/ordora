-- =============================================================
-- 0063_booking_staff_durations.sql
--
-- Phase 1 booking growth: per-service durations + staff calendars.
--
-- - products.booking_duration_min (nullable): a service's own visit
--   length; NULL falls back to the org default. Surfaced on the
--   public catalog (get_public_products) so the picker and slot grid
--   use it. Validation lives in app code (0 or 15–480), like the
--   other merchandising fields.
-- - bookings.staff_user_id already exists; it now carries meaning.
--   Overlap is scoped: a named pick conflicts with same-stylist and
--   unassigned visits; "anyone" keeps the conservative org-wide
--   check (it may reject a physically free slot, but never books
--   two visits onto one stylist).
-- - get_booking_busy gains the assignee column; get_booking_staff
--   lists bookable team members (active writers, owner first).
-- - create_booking is dropped and recreated (shape change, same
--   convention as 0041/0052/0055): + p_staff_user_id, duration
--   derived from the service row (org default fallback), staff must
--   be an active writer, scoped overlap.
-- =============================================================

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS booking_duration_min INT
    CHECK (booking_duration_min IS NULL OR booking_duration_min BETWEEN 15 AND 480);

-- -------------------------------------------------------------
-- Public catalog + visit length.
-- -------------------------------------------------------------
DROP FUNCTION IF EXISTS get_public_products(citext);

CREATE OR REPLACE FUNCTION get_public_products(p_slug citext)
RETURNS TABLE (
    product_id          uuid,
    product_name        text,
    product_description text,
    product_short       text,
    product_material    text,
    product_category    text,
    unit_price_minor    bigint,
    currency            text,
    quote_only          boolean,
    starting_from       boolean,
    availability        text,
    cover_image_id      uuid,
    product_questions   jsonb,
    product_specs       text,
    product_color       text,
    product_production_days int,
    product_image_ids   text,
    booking_duration_min integer
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT p.id, p.name,
               COALESCE(p.description, ''),
               COALESCE(p.short_description, ''),
               COALESCE(p.material, ''),
               COALESCE(p.category, ''),
               p.unit_price_minor, p.currency::text,
               p.quote_only, p.starting_from, p.availability,
               (SELECT a.id FROM attachments a
                 WHERE a.entity_type = 'PRODUCT'
                   AND a.entity_id = p.id
                   AND a.purpose = 'product_cover'
                 ORDER BY a.created_at DESC
                 LIMIT 1),
               COALESCE(p.questions, '[]'),
               COALESCE(p.specs, ''),
               COALESCE(p.color, ''),
               COALESCE(p.production_days, 0),
               COALESCE((
                   SELECT string_agg(a.id::text, ',' ORDER BY (a.purpose = 'product_cover') DESC, a.created_at DESC)
                     FROM attachments a
                    WHERE a.entity_type = 'PRODUCT'
                      AND a.entity_id = p.id
                      AND a.purpose IN ('product_gallery', 'product_cover')
               ), ''),
               COALESCE(p.booking_duration_min, 0)
        FROM products p
        JOIN organizations o ON o.id = p.organization_id
        WHERE o.slug = p_slug
          AND o.suspended_at IS NULL
          AND p.active
          AND NOT p.hidden
          AND p.availability <> 'out_of_stock'
        ORDER BY p.name ASC
        LIMIT 50;
END;
$$;

REVOKE ALL ON FUNCTION get_public_products(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_products(citext) TO ordora_app;

-- -------------------------------------------------------------
-- Busy blocks now carry the assignee (NULL = shared pool).
-- -------------------------------------------------------------
DROP FUNCTION IF EXISTS get_booking_busy(citext, timestamptz, timestamptz);

CREATE OR REPLACE FUNCTION get_booking_busy(p_slug citext, p_from timestamptz, p_to timestamptz)
RETURNS TABLE (
    start_at   timestamptz,
    end_at     timestamptz,
    buf_before integer,
    buf_after  integer,
    staff_id   uuid
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
        SELECT b.start_at, b.end_at, b.buf_before_min, b.buf_after_min,
               b.staff_user_id
          FROM bookings b
         WHERE b.organization_id = v_org_id
           AND b.status IN ('REQUESTED','CONFIRMED')
           AND tstzrange(b.start_at, b.end_at, '[)') &&
               tstzrange(p_from, p_to, '[)')
         ORDER BY b.start_at;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_busy(citext, timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_busy(citext, timestamptz, timestamptz) TO ordora_app;

-- -------------------------------------------------------------
-- Bookable team: active writers (owner first, then by name).
-- Viewers never take appointments.
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION get_booking_staff(p_slug citext)
RETURNS TABLE (
    user_id uuid,
    name    text
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
        SELECT m.user_id, u.name
          FROM organization_members m
          JOIN users u ON u.id = m.user_id
         WHERE m.organization_id = v_org_id
           AND m.status = 'ACTIVE'
           AND m.role <> 'VIEWER'
         ORDER BY (m.role = 'OWNER') DESC, u.name ASC;
END;
$$;

REVOKE ALL ON FUNCTION get_booking_staff(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_booking_staff(citext) TO ordora_app;

-- -------------------------------------------------------------
-- create_booking with per-service duration + staff scoping.
-- Dropped first: new OUT/IN shape (same convention as 0041).
-- Duration derives from the service row (org default fallback) and
-- the invariant block enforces it, so a spoofed length fails here
-- no matter what the caller computed.
-- -------------------------------------------------------------
DROP FUNCTION IF EXISTS create_booking(citext, text, citext, text, text, text, uuid, timestamptz, timestamptz, text, integer, integer, bytea, uuid, uuid, uuid, uuid);

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
    p_booking_id   uuid,
    p_staff_user_id uuid
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
    v_timezone  text;
    v_flag      boolean := FALSE;
    v_ov        boolean;
    v_accepting boolean := TRUE;
    v_notice    integer := 2;
    v_advance   integer := 30;
    v_duration  integer := 60;
    v_buf_b     integer := 0;
    v_buf_a     integer := 0;
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

    SELECT o.id, COALESCE(o.business_category, ''), o.currency::text, o.timezone
      INTO v_org_id, v_category, v_currency, v_timezone
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
           COALESCE(s.max_advance_days, 30),
           COALESCE(s.default_duration_min, 60),
           COALESCE(s.buffer_before_min, 0),
           COALESCE(s.buffer_after_min, 0)
      INTO v_accepting, v_notice, v_advance, v_duration, v_buf_b, v_buf_a
      FROM (SELECT 1) AS one
      LEFT JOIN booking_settings s ON s.organization_id = v_org_id;
    IF NOT v_accepting THEN
        o_error := 'NOT_ACCEPTING'; RETURN NEXT; RETURN;
    END IF;

    -- Visit length: the service row wins, else the org default above.
    SELECT COALESCE(prod.booking_duration_min, v_duration) INTO v_duration
      FROM (SELECT 1) AS one
      LEFT JOIN products prod ON prod.id = p_product_id
                             AND prod.organization_id = v_org_id;

    -- Caller-computed invariants: exact duration, configured buffers,
    -- org timezone. The app computes all three; the database holds
    -- the line if any caller ever doesn't.
    IF p_tz IS DISTINCT FROM v_timezone
       OR (p_end - p_start) <> make_interval(mins => v_duration)
       OR p_buf_before <> v_buf_b
       OR p_buf_after <> v_buf_a THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
    END IF;

    -- The assignee must be an active writer on the team (or nobody).
    IF p_staff_user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM organization_members m
         WHERE m.organization_id = v_org_id
           AND m.user_id = p_staff_user_id
           AND m.status = 'ACTIVE'
           AND m.role <> 'VIEWER') THEN
        o_error := 'BAD_SLOT'; RETURN NEXT; RETURN;
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

    -- Overlap, with buffers on both sides. Only live bookings block, and
    -- only within the same lane: a named pick conflicts with same-stylist
    -- and unassigned visits; "anyone" keeps the conservative org-wide
    -- check (it may reject a physically free slot, never double-books).
    PERFORM 1 FROM bookings b
     WHERE b.organization_id = v_org_id
       AND b.status IN ('REQUESTED','CONFIRMED')
       AND (p_staff_user_id IS NULL
            OR b.staff_user_id IS NULL
            OR b.staff_user_id = p_staff_user_id)
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
         staff_user_id, start_at, end_at, timezone, buf_before_min,
         buf_after_min, status, customer_notes, created_at, updated_at)
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

REVOKE ALL ON FUNCTION create_booking(citext, text, citext, text, text, text, uuid, timestamptz, timestamptz, text, integer, integer, bytea, uuid, uuid, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_booking(citext, text, citext, text, text, text, uuid, timestamptz, timestamptz, text, integer, integer, bytea, uuid, uuid, uuid, uuid, uuid) TO ordora_app;
