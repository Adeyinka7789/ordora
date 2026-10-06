-- =============================================================
-- 0047_asoebi_deep.sql
--
-- Deep Aso-ebi: one master group, many self-serve sub-orders.
-- NO payment gateway: Paid/Collected are manual tick-boxes.
-- Bride/groom manages money via secret manage link; tailor sews.
--
-- 1. order_groups: fixed price per head, fabric, template, tokens
--    - price_minor / currency: fixed per-member price (0 = tailor quotes)
--    - fabric: e.g. "Wine Lace" (shown on join form)
--    - measurement_template_id: default template for join form
--    - join_token_hash: public join link /g/{raw} (guests submit)
--    - manage_token_hash: secret bride link /g/{raw}/manage?key={raw}
--    - join_enabled: tailor/bride can close intake
-- 2. orders: manual ticks (no money moves)
--    - member_paid / member_paid_at: bride's tick (cash collected offline)
--    - collected: garment picked up (tailor or bride ticks)
-- 3. order_measurements.extra_values JSONB: guest-added customs
--    (cap size, gele size...) — free-form, no template validation.
-- =============================================================

-- 1. Master group columns.
ALTER TABLE order_groups
    ADD COLUMN IF NOT EXISTS price_minor BIGINT NOT NULL DEFAULT 0 CHECK (price_minor >= 0),
    ADD COLUMN IF NOT EXISTS currency CHAR(3) NOT NULL DEFAULT 'NGN',
    ADD COLUMN IF NOT EXISTS fabric TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS measurement_template_id UUID REFERENCES measurement_templates(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS join_slug TEXT,
    ADD COLUMN IF NOT EXISTS join_token_hash BYTEA,
    ADD COLUMN IF NOT EXISTS manage_token_hash BYTEA,
    ADD COLUMN IF NOT EXISTS join_enabled BOOLEAN NOT NULL DEFAULT TRUE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_groups_join_slug
    ON order_groups(join_slug) WHERE join_slug IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_order_groups_join_token
    ON order_groups(join_token_hash) WHERE join_token_hash IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_order_groups_manage_token
    ON order_groups(manage_token_hash) WHERE manage_token_hash IS NOT NULL;

-- Backfill tokens for existing groups so old links keep working once
-- the app mints raw tokens on next view (NULL = not yet shared).
-- Nothing else to backfill: price 0 = "tailor quotes", join open.

-- 2. Manual ticks on member orders.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS member_paid BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS member_paid_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS collected BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS collected_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_orders_group_paid
    ON orders(group_id, member_paid) WHERE group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_orders_group_collected
    ON orders(group_id, collected) WHERE group_id IS NOT NULL;

-- 3. Guest-added custom measurements (cap size, gele size...).
ALTER TABLE order_measurements
    ADD COLUMN IF NOT EXISTS extra_values JSONB NOT NULL DEFAULT '{}'::jsonb;

-- ---------- Public join lookup (no login) ----------
-- Join slug is public (printed in WhatsApp groups); manage key stays hashed.
CREATE OR REPLACE FUNCTION lookup_group_join(p_slug TEXT)
RETURNS TABLE (
    group_id uuid,
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
        SELECT g.id, o.name, g.name, g.fabric, g.occasion_date,
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

-- ---------- Public member submit (no login) ----------
-- Creates customer (by phone, else name) + sub-order with fixed price
-- + measurement (template values + extra customs + style notes).
-- Returns the new order number for the success page.
CREATE OR REPLACE FUNCTION create_group_member_order(
    p_slug            TEXT,
    p_customer_name   TEXT,
    p_customer_phone  TEXT,
    p_values          JSONB,
    p_extra_values    JSONB,
    p_style_notes     TEXT,
    p_new_order_id    UUID,
    p_new_customer_id UUID,
    p_new_meas_id     UUID,
    p_audit_id        UUID
)
RETURNS TABLE (order_number text, org_name text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_group_id      UUID;
    v_org_id        UUID;
    v_org_name      TEXT;
    v_currency      CHAR(3);
    v_price         BIGINT;
    v_fabric        TEXT;
    v_group_name    TEXT;
    v_template_id   UUID;
    v_enabled       BOOLEAN;
    v_owner_id      UUID;
    v_customer_id   UUID;
    v_order_number  TEXT;
    v_seq           BIGINT;
    v_year          INT;
    v_tmpl_gender   TEXT;
    v_tmpl_garment  TEXT;
    v_tmpl_name     TEXT;
    v_tmpl_fields   JSONB;
BEGIN
    -- 1. Resolve group + org.
    SELECT g.id, g.organization_id, o.name, g.currency, g.price_minor,
           g.fabric, g.name, g.measurement_template_id, g.join_enabled
      INTO v_group_id, v_org_id, v_org_name, v_currency, v_price,
           v_fabric, v_group_name, v_template_id, v_enabled
      FROM order_groups g
      JOIN organizations o ON o.id = g.organization_id
     WHERE g.join_slug = p_slug
     LIMIT 1;

    IF v_group_id IS NULL THEN RETURN; END IF;
    IF NOT v_enabled THEN RETURN; END IF;

    -- 2. Attribute to org owner (same as public intake).
    SELECT user_id INTO v_owner_id
      FROM organization_members
     WHERE organization_id = v_org_id AND role = 'OWNER'
     ORDER BY created_at ASC LIMIT 1;
    IF v_owner_id IS NULL THEN
        SELECT user_id INTO v_owner_id
          FROM organization_members
         WHERE organization_id = v_org_id AND status = 'ACTIVE'
         ORDER BY created_at ASC LIMIT 1;
    END IF;
    IF v_owner_id IS NULL THEN RETURN; END IF;

    -- 3. Find-or-create customer by phone (fallback: name).
    IF NULLIF(p_customer_phone, '') IS NOT NULL THEN
        SELECT id INTO v_customer_id FROM customers
         WHERE organization_id = v_org_id AND phone = p_customer_phone
         LIMIT 1;
    END IF;
    IF v_customer_id IS NULL THEN
        SELECT id INTO v_customer_id FROM customers
         WHERE organization_id = v_org_id AND name = p_customer_name
           AND COALESCE(phone, '') = COALESCE(NULLIF(p_customer_phone, ''), '')
         LIMIT 1;
    END IF;
    IF v_customer_id IS NULL THEN
        INSERT INTO customers (id, organization_id, name, phone, created_at, updated_at)
        VALUES (p_new_customer_id, v_org_id, p_customer_name,
                NULLIF(p_customer_phone, ''), now(), now())
        RETURNING id INTO v_customer_id;
    END IF;

    -- 4. Order number.
    v_year := EXTRACT(YEAR FROM now())::int;
    INSERT INTO order_counters (organization_id, year, last_sequence, updated_at)
    VALUES (v_org_id, v_year, 1, now())
    ON CONFLICT (organization_id, year)
    DO UPDATE SET last_sequence = order_counters.last_sequence + 1,
                  updated_at = EXCLUDED.updated_at
    RETURNING last_sequence INTO v_seq;
    v_order_number := 'ORD-' || v_year::text || '-' || LPAD(v_seq::text, 6, '0');

    -- 5. Sub-order: title = group name, one item at fixed price.
    INSERT INTO orders (
        id, organization_id, customer_id, order_number, group_id,
        title, description, status, currency,
        subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
        member_paid, collected,
        created_by, created_at, updated_at
    )
    VALUES (
        p_new_order_id, v_org_id, v_customer_id, v_order_number, v_group_id,
        LEFT(v_group_name, 200), LEFT(COALESCE(NULLIF(p_style_notes, ''), v_fabric), 2000),
        'NEW', v_currency,
        v_price, 0, 0, v_price, 0,
        FALSE, FALSE,
        v_owner_id, now(), now()
    );

    IF v_price > 0 THEN
        INSERT INTO order_items (id, organization_id, order_id, description, quantity, unit_price_minor, subtotal_minor, position, created_at)
        VALUES (gen_random_uuid(), v_org_id, p_new_order_id,
                LEFT(COALESCE(NULLIF(v_fabric, ''), v_group_name), 200),
                1.000, v_price, v_price, 0, now());
    END IF;

    -- 6. Measurement row (template snapshot when set; extras always stored).
    IF v_template_id IS NOT NULL THEN
        SELECT gender::text, garment, name, fields
          INTO v_tmpl_gender, v_tmpl_garment, v_tmpl_name, v_tmpl_fields
          FROM measurement_templates WHERE id = v_template_id;
    END IF;

    INSERT INTO order_measurements (
        id, organization_id, order_id, template_id, gender, garment,
        template_name, "values", notes, template_snapshot, extra_values,
        created_by, created_at, updated_at
    )
    VALUES (
        p_new_meas_id, v_org_id, p_new_order_id, v_template_id,
        COALESCE(v_tmpl_gender, 'unisex'), COALESCE(v_tmpl_garment, 'aso-ebi'),
        COALESCE(v_tmpl_name, 'Aso-ebi'),
        COALESCE(p_values, '{}'::jsonb),
        LEFT(COALESCE(p_style_notes, ''), 2000),
        CASE WHEN v_template_id IS NULL THEN '{}'::jsonb
             ELSE jsonb_build_object('name', v_tmpl_name, 'gender', v_tmpl_gender,
                                    'garment', v_tmpl_garment, 'fields', COALESCE(v_tmpl_fields, '[]'::jsonb))
        END,
        COALESCE(p_extra_values, '{}'::jsonb),
        v_owner_id, now(), now()
    )
    ON CONFLICT (order_id) DO NOTHING;

    -- 7. Audit.
    INSERT INTO audit_logs (id, organization_id, actor_user_id, action, entity_type, entity_id, after, created_at)
    VALUES (p_audit_id, v_org_id, NULL, 'group.member_joined', 'ORDER', p_new_order_id,
            jsonb_build_object('source', 'asoebi_join', 'group_id', v_group_id), now());

    RETURN QUERY SELECT v_order_number, v_org_name;
END;
$$;

REVOKE ALL ON FUNCTION create_group_member_order(TEXT, TEXT, TEXT, JSONB, JSONB, TEXT, UUID, UUID, UUID, UUID) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_group_member_order(TEXT, TEXT, TEXT, JSONB, JSONB, TEXT, UUID, UUID, UUID, UUID) TO ordora_app;

-- ---------- Bride manage: lookup by secret ----------
CREATE OR REPLACE FUNCTION lookup_group_manage(p_manage_hash BYTEA)
RETURNS TABLE (
    group_id uuid,
    org_name text,
    group_name text,
    fabric text,
    occasion_date date,
    price_minor bigint,
    currency char(3)
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT g.id, o.name, g.name, g.fabric, g.occasion_date, g.price_minor, g.currency
        FROM order_groups g
        JOIN organizations o ON o.id = g.organization_id
        WHERE g.manage_token_hash = p_manage_hash
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_group_manage(BYTEA) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_group_manage(BYTEA) TO ordora_app;

-- ---------- Bride manage: one-click Paid/Unpaid (no gateway) ----------
-- Flips orders.member_paid only. Money never moves here.
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
    v_ok BOOLEAN := FALSE;
BEGIN
    SELECT id INTO v_group_id FROM order_groups
     WHERE manage_token_hash = p_manage_hash LIMIT 1;
    IF v_group_id IS NULL THEN RETURN FALSE; END IF;

    UPDATE orders
       SET member_paid = p_paid,
           member_paid_at = CASE WHEN p_paid THEN now() ELSE NULL END,
           updated_at = now()
     WHERE id = p_order_id AND group_id = v_group_id;

    GET DIAGNOSTICS v_ok = ROW_COUNT;
    RETURN v_ok > 0;
END;
$$;

REVOKE ALL ON FUNCTION set_group_member_paid(BYTEA, UUID, BOOLEAN) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION set_group_member_paid(BYTEA, UUID, BOOLEAN) TO ordora_app;
