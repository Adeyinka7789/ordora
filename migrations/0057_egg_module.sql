-- =============================================================
-- 0057_egg_module.sql
--
-- Perishable-goods module (poultry/egg sellers, food vendors):
-- daily capacity cap + transfer-receipt ordering on top of the
-- existing orders/payments/attachments tables. Only two new
-- tables; egg orders ARE orders (statuses map onto order_status,
-- refunds use payment reversals).
--
-- Tables are tenant-scoped (RLS) like everything else. The three
-- functions below are pre-auth (public intake has no tenant), so
-- they are SECURITY DEFINER with row_security off — the same
-- pattern as list_user_memberships (0005) and
-- get_team_invite_by_token (0056). Only ordora_app may call them.
-- =============================================================

-- Kill switch + pilot gate. Seeded OFF: enable per-org via
-- feature_flag_overrides for the pilot, flip on for rollout.
-- (The Go flag provider also honors % rollouts for route gating;
-- the intake config below treats the flag as on when overridden
-- or fully enabled, which covers pilot + rollout.)
INSERT INTO feature_flags(flag_key, name, description, enabled, rollout_percent)
VALUES ('egg_module', 'Egg ordering module',
        'Daily-cap + transfer-receipt ordering for poultry and food vendors',
        FALSE, 0)
ON CONFLICT DO NOTHING;

-- -------------------------------------------------------------
-- Per-seller egg settings (one row per org, created on first save).
-- -------------------------------------------------------------
CREATE TABLE poultry_settings (
    organization_id   UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    max_crates_per_day INT NOT NULL DEFAULT 100 CHECK (max_crates_per_day > 0),
    accepting_orders  BOOLEAN NOT NULL DEFAULT TRUE,
    bank_name         TEXT NOT NULL DEFAULT '',
    account_number    TEXT NOT NULL DEFAULT '',
    account_name      TEXT NOT NULL DEFAULT '',
    whatsapp_number   TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE poultry_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE poultry_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY poultry_settings_tenant ON poultry_settings
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Daily crate counter. One integer per (org, delivery day): this
-- is a capacity cap, NOT a stock ledger. PENDING + CONFIRMED
-- orders consume it at submit time; reject/cancel releases it.
-- The upsert takes a row lock held to commit, which serializes
-- concurrent submissions (same idea as order_counters).
-- -------------------------------------------------------------
CREATE TABLE daily_crate_counters (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    day             DATE NOT NULL,
    sold            NUMERIC(12,3) NOT NULL DEFAULT 0 CHECK (sold >= 0),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, day)
);

ALTER TABLE daily_crate_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE daily_crate_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY daily_crate_counters_tenant ON daily_crate_counters
    USING (organization_id = current_org_id())
    WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- Public intake config for one slug: everything the anonymous
-- order form needs (gate, cap, sold counts, bank block). Today /
-- tomorrow are derived from the org's own timezone inside the
-- function, so callers pass only the slug. Missing settings /
-- counters read as defaults. Suspended or unknown slugs return no
-- rows (caller 404s).
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION get_perishable_intake_config(p_slug citext)
RETURNS TABLE (
    org_id         uuid,
    is_perishable  boolean,
    flag_on        boolean,
    accepting      boolean,
    cap            integer,
    sold_today     numeric,
    sold_tomorrow  numeric,
    bank_name      text,
    account_number text,
    account_name   text,
    whatsapp       text,
    org_timezone   text,
    currency       text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_org_id    uuid;
    v_category  text;
    v_timezone  text;
    v_currency  text;
    v_today     date;
    v_tomorrow  date;
    v_flag      boolean := FALSE;
    v_ov        boolean;
BEGIN
    SET LOCAL row_security = off;

    SELECT o.id, COALESCE(o.business_category, ''), o.timezone, o.currency::text
      INTO v_org_id, v_category, v_timezone, v_currency
      FROM organizations o
     WHERE o.slug = p_slug
       AND o.suspended_at IS NULL
     LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    v_today := (now() AT TIME ZONE v_timezone)::date;
    v_tomorrow := v_today + 1;

    -- Pilot gate: per-org override wins, else the flag must be fully on.
    SELECT ov.enabled INTO v_ov
      FROM feature_flag_overrides ov
     WHERE ov.flag_key = 'egg_module' AND ov.organization_id = v_org_id;
    IF FOUND THEN
        v_flag := v_ov;
    ELSE
        SELECT f.enabled AND f.rollout_percent >= 100 INTO v_flag
          FROM feature_flags f WHERE f.flag_key = 'egg_module';
        v_flag := COALESCE(v_flag, FALSE);
    END IF;

    RETURN QUERY
        SELECT v_org_id,
               (v_category IN ('Poultry & Eggs', 'Food & Catering')),
               v_flag,
               COALESCE(s.accepting_orders, TRUE),
               COALESCE(s.max_crates_per_day, 100),
               COALESCE(t.sold, 0),
               COALESCE(m.sold, 0),
               COALESCE(s.bank_name, ''),
               COALESCE(s.account_number, ''),
               COALESCE(s.account_name, ''),
               COALESCE(s.whatsapp_number, ''),
               v_timezone,
               v_currency
          FROM (SELECT 1) AS one
          LEFT JOIN poultry_settings s ON s.organization_id = v_org_id
          LEFT JOIN daily_crate_counters t
                 ON t.organization_id = v_org_id AND t.day = v_today
          LEFT JOIN daily_crate_counters m
                 ON m.organization_id = v_org_id AND m.day = v_tomorrow;
END;
$$;

REVOKE ALL ON FUNCTION get_perishable_intake_config(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_perishable_intake_config(citext) TO ordora_app;

-- -------------------------------------------------------------
-- Atomically consume cap and report the new total. The upsert
-- holds the counter row lock until the caller's commit, so two
-- concurrent submissions serialize here: the loser sees the
-- winner's increment. The caller (Go) compares sold_after to the
-- cap and rolls back on breach — the increment rolls back too.
-- Returns the org so the caller needs no second lookup.
-- -------------------------------------------------------------
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
            RETURNING daily_crate_counters.sold
        )
        SELECT (SELECT sold FROM up),
               (SELECT cap FROM s),
               (SELECT accepting FROM s),
               v_org_id;
END;
$$;

REVOKE ALL ON FUNCTION consume_crate_cap(citext, date, numeric) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION consume_crate_cap(citext, date, numeric) TO ordora_app;

-- -------------------------------------------------------------
-- Attach a customer-uploaded transfer receipt to the order that
-- was just created in the same transaction. The repo wires the
-- function's order id straight into this call — order ids never
-- come from user input, so receipts can't be planted on other
-- people's orders. Purpose payment_proof is customer-visible
-- through the portal token (mirrors the app allowlist).
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION attach_intake_receipt(
    p_order_id  uuid,
    p_org_id    uuid,
    p_key       text,
    p_filename  text,
    p_mime      text,
    p_size      bigint,
    p_attach_id uuid
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_owner_id uuid;
BEGIN
    SET LOCAL row_security = off;

    IF p_key IS NULL OR p_key = '' THEN
        RETURN;
    END IF;

    SELECT m.user_id INTO v_owner_id
      FROM organization_members m
     WHERE m.organization_id = p_org_id
       AND m.status = 'ACTIVE'
     ORDER BY (m.role = 'OWNER') DESC, m.created_at ASC
     LIMIT 1;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'no active member for org';
    END IF;

    INSERT INTO attachments
        (id, organization_id, entity_type, entity_id, storage_key,
         filename, mime_type, size_bytes, purpose, uploaded_by, created_at)
    VALUES (p_attach_id, p_org_id, 'ORDER', p_order_id, p_key,
            p_filename, p_mime, p_size, 'payment_proof', v_owner_id, now());
END;
$$;

REVOKE ALL ON FUNCTION attach_intake_receipt(uuid, uuid, text, text, text, bigint, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION attach_intake_receipt(uuid, uuid, text, text, text, bigint, uuid) TO ordora_app;
