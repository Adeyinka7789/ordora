-- =============================================================
-- 0052_product_catalog_portal.sql
--
-- Rich product catalog + customer portal proofs.
--
-- 1. products: catalog merchandising columns. Visibility stays on the
--    existing `active` flag (false = archived) plus a new `hidden`
--    flag (active but not shown publicly). Derived visibility:
--    archived (!active) / hidden (hidden) / active.
-- 2. order_items: product snapshot columns (product_id link + material
--    + image ref) so later catalog edits never rewrite history.
-- 3. orders: public_answers JSONB for product-specific question answers
--    given on the public intake form.
-- 4. attachments: entity_type gains PRODUCT for gallery/cover images.
-- 5. get_public_products: richer catalog rows (material, short
--    description, category, quote/availability flags, cover image),
--    hides hidden/out-of-stock items and suspended orgs.
-- 6. create_public_order: snapshots product_id/material/image, accepts
--    answers JSONB (stored + appended to the description for staff).
-- 7. get_public_order_items: adds material + image_ref.
-- 8. get_public_order_payments: adds proof_ids aligned with proof_names,
--    restricted to customer-visible purposes.
-- 9. get_portal_attachment: token-bound proof download metadata. Checks
--    token validity, org match, order/payment ownership, and purpose.
-- 10. get_public_product_image: public cover/gallery image bytes lookup.
-- =============================================================

-- ---------- 1. products catalog columns ----------
ALTER TABLE products ADD COLUMN IF NOT EXISTS material TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS color TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS short_description TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS internal_notes TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS specs TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS production_days INT CHECK (production_days IS NULL OR production_days >= 0);
ALTER TABLE products ADD COLUMN IF NOT EXISTS quote_only BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE products ADD COLUMN IF NOT EXISTS starting_from BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE products ADD COLUMN IF NOT EXISTS hidden BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE products ADD COLUMN IF NOT EXISTS availability TEXT NOT NULL DEFAULT 'in_stock'
    CHECK (availability IN ('in_stock', 'low_stock', 'out_of_stock', 'made_to_order'));
ALTER TABLE products ADD COLUMN IF NOT EXISTS category TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS questions JSONB NOT NULL DEFAULT '[]';

CREATE INDEX IF NOT EXISTS idx_products_org_category
    ON products(organization_id, category) WHERE active;

-- ---------- 2. order_items snapshot columns ----------
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS product_id UUID;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS material TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS image_ref TEXT;

CREATE INDEX IF NOT EXISTS idx_order_items_product ON order_items(product_id) WHERE product_id IS NOT NULL;

-- ---------- 3. orders public answers ----------
ALTER TABLE orders ADD COLUMN IF NOT EXISTS public_answers JSONB NOT NULL DEFAULT '{}';

-- ---------- 4. attachments: PRODUCT entity ----------
-- The 0001 inline CHECK was auto-named attachments_entity_type_check,
-- so the replacement uses a distinct name. Idempotent: safe to re-run.
-- NOTE: match on %entity_type% only — Postgres stores IN-lists as
-- `= ANY (ARRAY[...])`, so an '%IN%' pattern never matches.
DO $$
DECLARE
    cname text;
BEGIN
    FOR cname IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'attachments'::regclass
           AND contype = 'c'
           AND conname <> 'attachments_entity_type_product_check'
           AND pg_get_constraintdef(oid) ILIKE '%entity_type%'
    LOOP
        EXECUTE format('ALTER TABLE attachments DROP CONSTRAINT %I', cname);
    END LOOP;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conrelid = 'attachments'::regclass
           AND conname = 'attachments_entity_type_product_check'
    ) THEN
        ALTER TABLE attachments
            ADD CONSTRAINT attachments_entity_type_product_check
            CHECK (entity_type IN ('ORDER', 'PAYMENT', 'CUSTOMER', 'PRODUCT'));
    END IF;
END
$$;

-- ---------- 5. get_public_products ----------
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
    product_questions   jsonb
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
                   AND a.purpose = 'PRODUCT_COVER'
                 ORDER BY a.created_at DESC
                 LIMIT 1),
               COALESCE(p.questions, '[]')
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

-- ---------- 6. create_public_order (+answers, +snapshot) ----------
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
    p_audit_id          uuid,
    p_answers           jsonb
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
    v_ans            record;
    v_item_id        uuid;
    v_product_id     uuid;
    v_qty            numeric;
    v_pname          text;
    v_price          bigint;
    v_pmaterial      text;
    v_pimage         uuid;
    v_line           bigint;
    v_email          citext;
    v_phone          text;
    v_answers        jsonb := '{}';
    v_acount         int := 0;
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

    -- 5b. Product-specific answers: store raw JSONB and append a readable
    -- Q&A block to the description so staff see it everywhere.
    IF p_answers IS NOT NULL AND jsonb_typeof(p_answers) = 'array' THEN
        v_answers := p_answers;
        FOR v_ans IN
            SELECT e AS elem FROM jsonb_array_elements(p_answers) e
        LOOP
            EXIT WHEN v_acount >= 50;
            v_desc := COALESCE(v_desc || E'\n', '') ||
                '• ' || LEFT(COALESCE(v_ans.elem->>'product', ''), 80) ||
                ' — ' || LEFT(COALESCE(v_ans.elem->>'q', ''), 120) ||
                ': ' || LEFT(COALESCE(v_ans.elem->>'a', ''), 500);
            v_acount := v_acount + 1;
        END LOOP;
    END IF;

    -- 6. Insert the order shell (totals filled in after the lines).
    INSERT INTO orders (
        id, organization_id, customer_id, order_number,
        title, description, status, currency,
        subtotal_minor, discount_minor, tax_minor, total_minor, amount_paid_minor,
        expected_completion, created_by, created_at, updated_at, public_answers
    )
    VALUES (
        p_new_order_id, v_org_id, v_customer_id, v_order_number,
        COALESCE(v_title, 'Public order'), v_desc,
        'NEW', v_currency,
        0, 0, 0, 0, 0,
        p_expected_date, v_owner_id, now(), now(), v_answers
    );

    -- 7. Product lines: snapshot name/price/material/cover image, ignore
    -- unknown, inactive, hidden, or out-of-stock products and absurd
    -- quantities (client input is untrusted).
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
            SELECT pr.name, pr.unit_price_minor,
                   COALESCE(pr.material, ''),
                   (SELECT a.id FROM attachments a
                     WHERE a.entity_type = 'PRODUCT'
                       AND a.entity_id = pr.id
                       AND a.purpose = 'PRODUCT_COVER'
                     ORDER BY a.created_at DESC
                     LIMIT 1)
              INTO v_pname, v_price, v_pmaterial, v_pimage
              FROM products pr
              WHERE pr.id = v_product_id
                AND pr.organization_id = v_org_id
                AND pr.active
                AND NOT pr.hidden
                AND pr.availability <> 'out_of_stock'
              LIMIT 1;
            IF NOT FOUND THEN
                CONTINUE;
            END IF;
            v_line := ROUND(v_qty * v_price)::bigint;
            INSERT INTO order_items (
                id, organization_id, order_id, description,
                quantity, unit_price_minor, subtotal_minor, position, created_at,
                product_id, material, image_ref
            )
            VALUES (
                v_item_id, v_org_id, p_new_order_id, v_pname,
                v_qty, v_price, v_line, v_pos, now(),
                v_product_id, NULLIF(v_pmaterial, ''), v_pimage::text
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

REVOKE ALL ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, jsonb, uuid, uuid, uuid, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION create_public_order(citext, text, citext, text, text, date, bigint, jsonb, uuid, uuid, uuid, jsonb) TO ordora_app;

-- ---------- 7. get_public_order_items (+snapshot cols) ----------
DROP FUNCTION IF EXISTS get_public_order_items(uuid);

CREATE OR REPLACE FUNCTION get_public_order_items(p_order_id uuid)
RETURNS TABLE (
    description      text,
    quantity         numeric,
    unit_price_minor bigint,
    subtotal_minor   bigint,
    currency         text,
    item_position    int,
    material         text,
    image_ref        text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT
            oi.description,
            oi.quantity,
            oi.unit_price_minor,
            oi.subtotal_minor,
            o.currency::text,
            oi.position,
            COALESCE(oi.material, ''),
            COALESCE(oi.image_ref, '')
        FROM order_items oi
        JOIN orders o ON o.id = oi.order_id
        WHERE oi.order_id = p_order_id
        ORDER BY oi.position ASC, oi.id ASC;
END;
$$;

REVOKE ALL ON FUNCTION get_public_order_items(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_order_items(uuid) TO ordora_app;

-- ---------- 8. get_public_order_payments (+proof ids, visible purposes) ----------
DROP FUNCTION IF EXISTS get_public_order_payments(uuid);

CREATE OR REPLACE FUNCTION get_public_order_payments(p_order_id uuid)
RETURNS TABLE (
    method          text,
    reference       text,
    paid_at         timestamptz,
    amount_minor    bigint,
    currency        char(3),
    notes           text,
    is_reversed     boolean,
    is_reversal     boolean,
    proof_count     int,
    proof_names     text,
    proof_ids       text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT
            p.method::text,
            COALESCE(p.reference, ''),
            p.paid_at,
            p.amount_minor,
            p.currency,
            COALESCE(p.notes, ''),
            (p.reversed_by IS NOT NULL),
            (p.reverses IS NOT NULL),
            COUNT(a.id)::int,
            COALESCE(string_agg(a.filename, ', ' ORDER BY a.created_at), ''),
            COALESCE(string_agg(a.id::text, ',' ORDER BY a.created_at), '')
        FROM payments p
        LEFT JOIN attachments a
            ON a.entity_type = 'PAYMENT'
            AND a.entity_id = p.id
            AND a.purpose IN ('general', 'inspiration', 'payment_proof')
        WHERE p.order_id = p_order_id
        GROUP BY p.id
        ORDER BY p.paid_at DESC, p.created_at DESC;
END;
$$;

REVOKE ALL ON FUNCTION get_public_order_payments(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_order_payments(uuid) TO ordora_app;

-- ---------- 9. get_portal_attachment ----------
-- Token-bound proof download metadata. Returns one row only when:
--   1. the token resolves to a non-revoked order,
--   2. the attachment is in the same org, and
--   3. it belongs to the order (ORDER entity) or one of its payments
--      (PAYMENT entity) with a customer-visible purpose.
-- The storage key is returned to the server only — never in URLs.
DROP FUNCTION IF EXISTS get_portal_attachment(bytea, uuid);

CREATE OR REPLACE FUNCTION get_portal_attachment(p_token_hash bytea, p_attachment_id uuid)
RETURNS TABLE (
    attachment_id   uuid,
    order_id        uuid,
    organization_id uuid,
    filename        text,
    mime_type       text,
    size_bytes      bigint,
    storage_key     text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_order_id uuid;
    v_org_id   uuid;
BEGIN
    SELECT o.id, o.organization_id INTO v_order_id, v_org_id
      FROM orders o
      WHERE o.public_token_hash = p_token_hash
        AND o.public_token_revoked_at IS NULL
      LIMIT 1;

    IF v_order_id IS NULL THEN
        RETURN;
    END IF;

    RETURN QUERY
        SELECT a.id, v_order_id, a.organization_id,
               a.filename, a.mime_type, a.size_bytes, a.storage_key
          FROM attachments a
          WHERE a.id = p_attachment_id
            AND a.organization_id = v_org_id
            AND a.purpose IN ('general', 'inspiration', 'payment_proof')
            AND (
                (a.entity_type = 'ORDER' AND a.entity_id = v_order_id)
                OR
                (a.entity_type = 'PAYMENT' AND EXISTS (
                    SELECT 1 FROM payments p
                     WHERE p.id = a.entity_id
                       AND p.order_id = v_order_id
                ))
            )
          LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION get_portal_attachment(bytea, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_portal_attachment(bytea, uuid) TO ordora_app;

-- ---------- 10. get_public_product_image ----------
-- Public catalog image lookup: only for attachments on active, visible
-- products of non-suspended orgs. No listing — single id lookup.
DROP FUNCTION IF EXISTS get_public_product_image(uuid);

CREATE OR REPLACE FUNCTION get_public_product_image(p_attachment_id uuid)
RETURNS TABLE (
    storage_key text,
    mime_type   text,
    filename    text,
    size_bytes  bigint
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    RETURN QUERY
        SELECT a.storage_key, a.mime_type, a.filename, a.size_bytes
          FROM attachments a
          JOIN products pr ON pr.id = a.entity_id
          JOIN organizations o ON o.id = pr.organization_id
          WHERE a.id = p_attachment_id
            AND a.entity_type = 'PRODUCT'
            AND a.organization_id = pr.organization_id
            AND pr.active
            AND NOT pr.hidden
            AND o.suspended_at IS NULL
          LIMIT 1;
END;
$$;

REVOKE ALL ON FUNCTION get_public_product_image(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION get_public_product_image(uuid) TO ordora_app;
