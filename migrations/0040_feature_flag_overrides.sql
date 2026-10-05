-- =============================================================
-- 0040_feature_flag_overrides.sql
--
-- Per-organization feature-flag overrides: force a flag on or off
-- for one tenant, regardless of the global switch and rollout %.
-- An absent row means "follow the global rule".
--
-- Deliberately NO RLS: like feature_flags, this is a platform-level
-- table. Reads are open to app code (evaluated in-process by the
-- flags.Provider snapshot); writes happen only through admin
-- handlers (audited), never from tenant request paths.
-- =============================================================

CREATE TABLE feature_flag_overrides (
    flag_key        TEXT NOT NULL REFERENCES feature_flags(flag_key) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    enabled         BOOLEAN NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (flag_key, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_flag_overrides_org
    ON feature_flag_overrides (organization_id);

INSERT INTO admin_browsable_tables (table_name, display_name, search_columns, order_by, order_dir) VALUES
    ('feature_flag_overrides', 'Feature Flag Overrides', ARRAY['flag_key'], 'updated_at', 'DESC')
ON CONFLICT (table_name) DO NOTHING;
