-- =============================================================
-- 0035_measurements.sql
--
-- Tailoring measurements.
--
-- measurement_templates: garment measurement forms. System rows have
-- organization_id NULL (visible to every tenant); a business can later
-- add its own rows scoped to its org. Gender is male/female/unisex,
-- garment is a slug like 'shirt'. Fields is an ordered JSON array of
-- {key,label,unit,required}.
--
-- order_measurements: one row per order capturing the values taken at
-- order creation time, plus a snapshot of gender/garment/template name
-- so old orders still read sensibly if a template changes.
-- =============================================================

CREATE TABLE measurement_templates (
    id              UUID PRIMARY KEY,
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    gender          TEXT NOT NULL CHECK (gender IN ('male', 'female', 'unisex')),
    garment         TEXT NOT NULL CHECK (garment <> ''),
    name            TEXT NOT NULL CHECK (name <> ''),
    fields          JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_system       BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT measurement_templates_system_org CHECK (
        (is_system AND organization_id IS NULL)
        OR (NOT is_system AND organization_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX idx_measurement_templates_system
    ON measurement_templates (gender, garment) WHERE organization_id IS NULL;
CREATE UNIQUE INDEX idx_measurement_templates_org
    ON measurement_templates (organization_id, gender, garment) WHERE organization_id IS NOT NULL;
CREATE INDEX idx_measurement_templates_lookup
    ON measurement_templates (gender, garment);

CREATE TABLE order_measurements (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    template_id     UUID NOT NULL REFERENCES measurement_templates(id) ON DELETE RESTRICT,
    gender          TEXT NOT NULL,
    garment         TEXT NOT NULL,
    template_name   TEXT NOT NULL,
    values          JSONB NOT NULL DEFAULT '{}'::jsonb,
    notes           TEXT NOT NULL DEFAULT '',
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT order_measurements_one_per_order UNIQUE (order_id)
);

CREATE INDEX idx_order_measurements_order ON order_measurements(order_id);
CREATE INDEX idx_order_measurements_org   ON order_measurements(organization_id);

-- RLS
ALTER TABLE measurement_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE measurement_templates FORCE  ROW LEVEL SECURITY;

-- Everyone can read system templates; tenants can fully manage their own.
CREATE POLICY measurement_templates_read ON measurement_templates
    FOR SELECT
    USING (organization_id IS NULL OR (organization_id = current_org_id() AND tenant_is_set()));
CREATE POLICY measurement_templates_write ON measurement_templates
    FOR ALL
    USING      (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set() AND NOT is_system);

ALTER TABLE order_measurements ENABLE ROW LEVEL SECURITY;
ALTER TABLE order_measurements FORCE  ROW LEVEL SECURITY;

CREATE POLICY order_measurements_tenant ON order_measurements
    USING      (organization_id = current_org_id() AND tenant_is_set())
    WITH CHECK (organization_id = current_org_id() AND tenant_is_set());

-- Grants for the app role.
GRANT SELECT, INSERT, UPDATE, DELETE ON measurement_templates TO ordora_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON order_measurements TO ordora_app;

-- -------------------------------------------------------------
-- System seed templates (stable IDs so code/tests can rely on
-- gender+garment lookups; IDs below are fixed literals).
-- Units are display hints only. Values are entered free-form.
-- -------------------------------------------------------------

INSERT INTO measurement_templates (id, organization_id, gender, garment, name, fields, is_system) VALUES
-- Male --
('11111111-1111-1111-1111-111111111111', NULL, 'male', 'shirt', 'Men''s Shirt',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"shirt_length","label":"Shirt length","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":false},{"key":"hip","label":"Hip / Seat","unit":"in","required":false}]'::jsonb, true),
('22222222-2222-2222-2222-222222222222', NULL, 'male', 'trouser', 'Men''s Trouser',
 '[{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip / Seat","unit":"in","required":true},{"key":"thigh","label":"Thigh","unit":"in","required":true},{"key":"knee","label":"Knee","unit":"in","required":false},{"key":"inseam","label":"Inseam","unit":"in","required":true},{"key":"outseam","label":"Outseam","unit":"in","required":true},{"key":"ankle","label":"Ankle / Hem","unit":"in","required":false}]'::jsonb, true),
('33333333-3333-3333-3333-333333333333', NULL, 'male', 'suit', 'Men''s Suit (2-piece)',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"jacket_length","label":"Jacket length","unit":"in","required":true},{"key":"waist","label":"Waist (trouser)","unit":"in","required":true},{"key":"hip","label":"Hip / Seat","unit":"in","required":true},{"key":"inseam","label":"Inseam","unit":"in","required":true},{"key":"outseam","label":"Outseam","unit":"in","required":true}]'::jsonb, true),
('44444444-4444-4444-4444-444444444444', NULL, 'male', 'agbada', 'Agbada',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"full_length","label":"Full length","unit":"in","required":true},{"key":"cap","label":"Cap size","unit":"in","required":false},{"key":"waist","label":"Waist","unit":"in","required":false}]'::jsonb, true),
('55555555-5555-5555-5555-555555555555', NULL, 'male', 'kaftan', 'Men''s Kaftan',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"length","label":"Length","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":false}]'::jsonb, true),
-- Female --
('66666666-6666-6666-6666-666666666666', NULL, 'female', 'dress', 'Dress',
 '[{"key":"bust","label":"Bust","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":false},{"key":"dress_length","label":"Dress length","unit":"in","required":true},{"key":"neck_depth","label":"Neck depth","unit":"in","required":false}]'::jsonb, true),
('77777777-7777-7777-7777-777777777777', NULL, 'female', 'blouse', 'Blouse',
 '[{"key":"bust","label":"Bust","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"blouse_length","label":"Blouse length","unit":"in","required":true}]'::jsonb, true),
('88888888-8888-8888-8888-888888888888', NULL, 'female', 'skirt', 'Skirt',
 '[{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"skirt_length","label":"Skirt length","unit":"in","required":true},{"key":"knee","label":"Knee","unit":"in","required":false}]'::jsonb, true),
('99999999-9999-9999-9999-999999999999', NULL, 'female', 'trouser', 'Women''s Trouser',
 '[{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"thigh","label":"Thigh","unit":"in","required":true},{"key":"inseam","label":"Inseam","unit":"in","required":true},{"key":"outseam","label":"Outseam","unit":"in","required":true},{"key":"ankle","label":"Ankle / Hem","unit":"in","required":false}]'::jsonb, true),
('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', NULL, 'female', 'gown', 'Gown',
 '[{"key":"bust","label":"Bust","unit":"in","required":true},{"key":"waist","label":"Waist","unit":"in","required":true},{"key":"hip","label":"Hip","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":false},{"key":"gown_length","label":"Gown length","unit":"in","required":true},{"key":"train","label":"Train","unit":"in","required":false}]'::jsonb, true),
-- Unisex --
('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', NULL, 'unisex', 'kaftan', 'Kaftan (Unisex)',
 '[{"key":"neck","label":"Neck","unit":"in","required":true},{"key":"chest","label":"Chest","unit":"in","required":true},{"key":"shoulder","label":"Shoulder","unit":"in","required":true},{"key":"sleeve","label":"Sleeve length","unit":"in","required":true},{"key":"length","label":"Length","unit":"in","required":true}]'::jsonb, true);
