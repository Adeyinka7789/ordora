-- =============================================================
-- 0046_public_intake_contact.sql
--
-- Email-optional public intake: phone/WhatsApp becomes a first-class
-- customer key. When no email is given, the customer is matched (or
-- created) by phone instead. Empty strings are stored as NULL so a
-- blank email never collides with another blank email.
--
-- Full function rewrite (same pattern as 0041/0042): all column
-- references stay table-qualified.
-- =============================================================

DROP FUNCTION IF EXISTS create_public_order(citext, text, citext, text, text, date, bigint, jsonb, uuid, uuid, uuid);

CREATE OR REPLACE FUNCTION create_public_order(
    p_slug              citext,
    p_customer_name     text,
    p_customer_email    citext,
    p_customer_phone    text,
    p_description       text,
    p_expected_date     date,
    p_budget_minor      bigint,
    p_items             jsonb,
    p_new_order_id      uuid,
    p_new_customer_id   uuid,
    p_audit_id          uuid
)
RETURNS TABLE (
    order_id      uuid,
    order_number  text,
    org_name      text,
    org_phone     text,
    total_minor   bigint,
    org_currency  text
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
    v_title          text;
    v_desc           text;
    v_subtotal       bigint := 0;
    v_count          int := 0;
    v_pos            int := 0;
    v_first_name     text;
    v_elem           record;
    v_item_id        uuid;
    v_product_id     uuid;
    v_qty            numeric;
    v_pname          text;
    v_price          bigint;
    v_line           bigint;
    v_email          citext;
    v_phone          text;
BEGIN
    -- 1. Resolve org by slug. Suspended orgs take no new orders.
    SELECT o.id, o.name, COALESCE(o.phone, ''), o.currency
      INTO v_org_id, v_org_name, v_org_phone, v_currency
      FROM organizations o
      WHERE o.slug = p_slug
        AND o.suspended_at IS NULL
      LIMIT 1;

    IF v_org_id IS NULL THEN
        RETURN;
    END IF;

    -- 2. Pick an owner (the first OWNER member of the org) to attribute the order to.
    SELECT m.user_id INTO v_owner_id
      FROM organization_members m
      WHERE m.organization_id = v_org_id AND m.role = 'OWNER'
      ORDER BY m.created_at ASC
      LIMIT 1;

    IF v_owner_id IS NULL THEN
        -- Fallback: any active member
        SELECT m.user_id INTO v_owner_id
          FROM organization_members m
          WHERE m.organization_id = v_org_id AND m.status = 'ACTIVE'
          ORDER BY m.created_at ASC
          LIMIT 1;
    END IF;

    IF v_owner_id IS NULL THEN
        RETURN;
    END IF;

    -- 3. Look up the customer by email when given, else by phone, else
    -- create. Blanks become NULL so they never false-match.
    v_email := NULLIF(BTRIM(p_customer_email::text), '')::citext;
    v_phone := NULLIF(BTRIM(COALESCE(p_customer_phone, '')), '');
    IF v_email IS NOT NULL THEN
        SELECT c.id INTO v_customer_id
          FROM customers c
          WHERE c.organization_id = v_org_id AND c.email = v_email
          LIMIT 1;
    ELSIF v_phone IS NOT NULL THEN
        SELECT c.id INTO v_customer_id
          FROM customers c
          WHERE c.organization_id = v_org_id AND c.phone = v_phone
          LIMIT 1;
    END IF;

    IF v_customer_id IS NULL THEN
        INSERT INTO customers (id, organization_id, name, email, phone, created_at, updated_at)
        VALUES (p_new_customer_id, v_org_id, p_customer_name, v_email, v_phone, now(), now())
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

    -- 5. Title and stored description.
    v_title := LEFT(NULLIF(TRIM(BOTH ' ' FROM COALESCE(p_description, '')), ''), 200);
    v_desc := NULLIF(TRIM(BOTH ' ' FROM COALESCE(p_description, '')), '');
    IF p_budget_minor IS NOT NULL THEN
        v_desc := COALESCE(v_desc || E'\n\n', '') || '— Budget mentioned: ' || (p_budget_minor / 100)::text;
    END IF;

    -- 6. Insert the order shell (totals filled in after the lines).
    INSERT INTO orders (
        id, organization_id, customer_id, order_number,
        title, description, status, currency,
        subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
        expected_completion, created_by, created_at, updated_at
    )
    VALUES (
        p_new_order_id, v_org_id, v_customer_id, v_order_number,
        COALESCE(v_title, 'Public order'), v_desc,
        'NEW', v_currency,
        0, 0, 0, 0, 0,
        p_expected_date, v_owner_id, now(), now()
    );

    -- 7. Product lines: snapshot the live price, ignore unknown/inactive
    -- products and absurd quantities (client input is untrusted).
    IF p_items IS NOT NULL AND jsonb_typeof(p_items) = 'array' THEN
        FOR v_elem IN
            SELECT e AS elem FROM jsonb_array_elements(p_items) e
        LOOP
            EXIT WHEN v_count >= 25;
            BEGIN
                v_item_id := (v_elem.elem->>'id')::uuid;
                v_product_id := (v_elem.elem->>'product_id')::uuid;
                v_qty := ROUND((v_elem.elem->>'qty')::numeric, 3);
            EXCEPTION WHEN OTHERS THEN
                CONTINUE;
            END;
            IF v_qty IS NULL OR v_qty <= 0 OR v_qty > 10000 THEN
                CONTINUE;
            END IF;
            SELECT pr.name, pr.unit_price_minor INTO v_pname, v_price
              FROM products pr
              WHERE pr.id = v_product_id
                AND pr.organization_id = v_org_id
                AND pr.active
              LIMIT 1;
            IF NOT FOUND THEN
                CONTINUE;
            END IF;
            v_line := ROUND(v_qty * v_price)::bigint;
            INSERT INTO order_items (
                id, organization_id, order_id, description,
                quantity, unit_price_minor, subtotal_minor, position, created_at
            )
            VALUES (
                v_item_id, v_org_id, p_new_order_id, v_pname,
                v_qty, v_price, v_line, v_pos, now()
            );
            IF v_first_name IS NULL THEN
                v_first_name := v_pname;
            END IF;
            v_subtotal := v_subtotal + v_line;
            v_count := v_count + 1;
            v_pos := v_pos + 1;
        END LOOP;
    END IF;

    -- 8. Totals + title fallback when there was no free text.
    IF v_title IS NULL THEN
        IF v_count = 1 THEN
            v_title := LEFT(v_first_name, 200);
        ELSIF v_count > 1 THEN
            v_title := LEFT(v_first_name || ' + ' || (v_count - 1)::text || ' more', 200);
        ELSE
            v_title := 'Public order';
        END IF;
    END IF;

    UPDATE orders
    SET title = v_title,
        subtotal_minor = v_subtotal,
        total_minor = v_subtotal,
        updated_at = now()
    WHERE id = p_new_order_id;

    -- 9. Audit entry.
    INSERT INTO audit_logs (id, organization_id, actor_user_id, action, entity_type, entity_id, after, created_at)
    VALUES (p_audit_id, v_org_id, NULL, 'order.created_public', 'ORDER', p_new_order_id,
            jsonb_build_object('source', 'public_intake', 'customer_email', p_customer_email,
                               'item_count', v_count, 'total_minor', v_subtotal), now());

    RETURN QUERY SELECT p_new_order_id, v_order_number, v_org_name, v_org_phone, v_subtotal, v_currency::text;
END;
$$;

REVOKE ALL ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, jsonb, uuid, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, jsonb, uuid, uuid, uuid) TO ordora_app;
