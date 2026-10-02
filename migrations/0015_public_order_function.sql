-- =============================================================
-- 0015_public_order_function.sql
--
-- The public intake form must create an order from an
-- unauthenticated request. This SECURITY DEFINER function does
-- that atomically, taking a slug + customer + description.
--
-- Returns: (order_id, order_number, org_name, org_phone)
-- =============================================================

-- Lookup for the intake page header (public).
CREATE OR REPLACE FUNCTION lookup_public_org(p_slug citext)
RETURNS TABLE (
    org_id   uuid,
    org_name text,
    currency char(3)
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT id, name, currency
        FROM organizations
        WHERE slug = p_slug
        LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION lookup_public_org(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lookup_public_org(citext) TO ordora_app;

-- Order creation from a public submission.
CREATE OR REPLACE FUNCTION create_public_order(
    p_slug              citext,
    p_customer_name     text,
    p_customer_email    citext,
    p_customer_phone    text,
    p_description       text,
    p_expected_date     date,
    p_budget_minor      bigint,
    p_new_order_id      uuid,
    p_new_customer_id   uuid,
    p_audit_id          uuid
)
RETURNS TABLE (
    order_id      uuid,
    order_number  text,
    org_name      text,
    org_phone     text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_org_id         uuid;
    v_org_name       text;
    v_org_phone      text;
    v_currency       char(3);
    v_owner_id       uuid;
    v_customer_id    uuid;
    v_order_number   text;
    v_seq            bigint;
    v_year           int;
BEGIN
    -- 1. Resolve org by slug.
    SELECT id, name, COALESCE(phone, ''), currency
      INTO v_org_id, v_org_name, v_org_phone, v_currency
      FROM organizations
      WHERE slug = p_slug
      LIMIT 1;

    IF v_org_id IS NULL THEN
        RETURN;
    END IF;

    -- 2. Pick an owner (the first OWNER member of the org) to attribute the order to.
    SELECT user_id INTO v_owner_id
      FROM organization_members
      WHERE organization_id = v_org_id AND role = 'OWNER'
      ORDER BY created_at ASC
      LIMIT 1;

    IF v_owner_id IS NULL THEN
        -- Fallback: any active member
        SELECT user_id INTO v_owner_id
          FROM organization_members
          WHERE organization_id = v_org_id AND status = 'ACTIVE'
          ORDER BY created_at ASC
          LIMIT 1;
    END IF;

    IF v_owner_id IS NULL THEN
        RETURN;
    END IF;

    -- 3. Look up the customer by (org, email), or create a new one.
    SELECT id INTO v_customer_id
      FROM customers
      WHERE organization_id = v_org_id AND email = p_customer_email
      LIMIT 1;

    IF v_customer_id IS NULL THEN
        INSERT INTO customers (id, organization_id, name, email, phone, created_at, updated_at)
        VALUES (p_new_customer_id, v_org_id, p_customer_name, p_customer_email,
                NULLIF(p_customer_phone, ''), now(), now())
        RETURNING id INTO v_customer_id;
    END IF;

    -- 4. Allocate an order number for (org, year).
    v_year := EXTRACT(YEAR FROM now())::int;
    INSERT INTO order_counters (organization_id, year, last_sequence, updated_at)
    VALUES (v_org_id, v_year, 1, now())
    ON CONFLICT (organization_id, year)
    DO UPDATE SET last_sequence = order_counters.last_sequence + 1,
                  updated_at = EXCLUDED.updated_at
    RETURNING last_sequence INTO v_seq;

    v_order_number := 'ORD-' || v_year::text || '-' || LPAD(v_seq::text, 6, '0');

    -- 5. Insert the order. Zero items, status NEW, totals zero.
    INSERT INTO orders (
        id, organization_id, customer_id, order_number,
        title, description, status, currency,
        subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
        expected_completion, created_by, created_at, updated_at
    )
    VALUES (
        p_new_order_id, v_org_id, v_customer_id, v_order_number,
        LEFT(p_description, 200),
        CASE WHEN p_budget_minor IS NOT NULL
             THEN p_description || E'\n\n— Budget mentioned: ' || (p_budget_minor / 100)::text
             ELSE p_description END,
        'NEW', v_currency,
        0, 0, 0, 0, 0,
        p_expected_date, v_owner_id, now(), now()
    );

    -- 6. Audit entry.
    INSERT INTO audit_logs (id, organization_id, actor_user_id, action, entity_type, entity_id, after, created_at)
    VALUES (p_audit_id, v_org_id, NULL, 'order.created_public', 'ORDER', p_new_order_id,
            jsonb_build_object('source', 'public_intake', 'customer_email', p_customer_email), now());

    RETURN QUERY SELECT p_new_order_id, v_order_number, v_org_name, v_org_phone;
END;
$$;

REVOKE ALL ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, uuid, uuid, uuid) TO ordora_app;
