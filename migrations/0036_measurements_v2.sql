-- =============================================================
-- 0036_measurements_v2.sql
--
-- Model C hardening: clone-and-customize + history-safe snapshots.
--
-- 1. order_measurements.template_snapshot JSONB
--    Stores {name, gender, garment, fields} at order-creation time so
--    editing or deleting a template never rewrites past orders.
--    Backfilled from the live template for existing rows.
-- 2. order_measurements.template_id becomes nullable ON DELETE SET NULL
--    so a custom template can be deleted without losing order history
--    (the snapshot keeps rendering).
-- 3. Relax tenant uniqueness: shops can own several templates for the
--    same gender+garment (e.g. "My Senator" + "Premium Senator").
--    Uniqueness is now (organization_id, name); system rows keep
--    (gender, garment) unique.
-- 4. Extra system seeds: senator, native (buba+sokoto), women's kaftan
--    and women's suit — 15 defaults total with 0035.
-- =============================================================

-- 1. Snapshot column (additive, safe).
ALTER TABLE order_measurements
    ADD COLUMN IF NOT EXISTS template_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Backfill snapshots for rows created before this migration.
UPDATE order_measurements om
SET template_snapshot = jsonb_build_object(
        'name', mt.name,
        'gender', mt.gender,
        'garment', mt.garment,
        'fields', COALESCE(mt.fields, '[]'::jsonb)
    )
FROM measurement_templates mt
WHERE mt.id = om.template_id
  AND (om.template_snapshot IS NULL OR om.template_snapshot = '{}'::jsonb);

-- 2. Allow template deletion without losing history.
ALTER TABLE order_measurements
    ALTER COLUMN template_id DROP NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'order_measurements_template_id_fkey'
    ) THEN
        ALTER TABLE order_measurements
            DROP CONSTRAINT order_measurements_template_id_fkey;
    END IF;
END
$$;

ALTER TABLE order_measurements
    ADD CONSTRAINT order_measurements_template_id_fkey
    FOREIGN KEY (template_id)
    REFERENCES measurement_templates(id)
    ON DELETE SET NULL;

-- 3. Tenants may own several templates per garment; names stay unique.
DROP INDEX IF EXISTS idx_measurement_templates_org;
CREATE UNIQUE INDEX IF NOT EXISTS idx_measurement_templates_org_name
    ON measurement_templates (organization_id, name) WHERE organization_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_measurement_templates_org_lookup
    ON measurement_templates (organization_id, gender, garment) WHERE organization_id IS NOT NULL;

-- 4. Extra system seeds (fixed IDs; idempotent).
INSERT INTO measurement_templates (id, organization_id, gender, garment, name, fields, is_system) VALUES
('cccccccc-cccc-cccc-cccc-cccccccccccc', NULL, 'male', 'senator', 'Men''s Senator',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"length","label":"Length","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":false},{"key":"hip","label":"Hip / Seat","unit":"in","required":false}]'::jsonb, true),
('dddddddd-dddd-dddd-dddd-dddddddddddd', NULL, 'male', 'native', 'Men''s Native (Buba + Sokoto)',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"buba_length","label":"Buba length","unit":"in","required":true},{"key":"waist","label":"Waist (sokoto)","unit":"in","required":true},{"key":"sokoto_length","label":"Sokoto length","unit":"in","required":true},{"key":"hip","label":"Hip / Seat","unit":"in","required":false}]'::jsonb, true),
('eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee', NULL, 'female', 'kaftan', 'Women''s Kaftan',
 '[{"key":"bust","label":"Bust","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":false},{"key":"length","label":"Length","unit":"in","required":true},{"key":"neck_depth","label":"Neck depth","unit":"in","required":false}]'::jsonb, true),
('ffffffff-ffff-ffff-ffff-ffffffffffff', NULL, 'female', 'suit', 'Women''s Suit',
 '[{"key":"bust","label":"Bust","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"jacket_length","label":"Jacket length","unit":"in","required":true},{"key":"skirt_length","label":"Skirt length","unit":"in","required":false},{"key":"inseam","label":"Inseam","unit":"in","required":false}]'::jsonb, true)
ON CONFLICT (id) DO NOTHING;

-- 5. Admin browser visibility (idempotent).
INSERT INTO admin_browsable_tables (table_name, display_name, search_columns, order_by, order_dir) VALUES
    ('measurement_templates', 'Measurement Templates', ARRAY['name','garment','gender'], 'created_at', 'DESC'),
    ('order_measurements',    'Order Measurements',    ARRAY['template_name','garment'],  'created_at', 'DESC')
ON CONFLICT (table_name) DO NOTHING;
