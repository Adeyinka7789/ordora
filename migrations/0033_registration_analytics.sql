-- =============================================================
-- 0033_registration_analytics.sql
--
-- Captures business profile details at registration for analytics:
-- business type/category (incl. free-text "Other"), team size and
-- referral source. Phone/address reuse the existing columns.
-- Nullable-free with defaults so existing rows stay valid.
-- =============================================================

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS business_type    text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS business_category text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS team_size        text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS referral_source  text NOT NULL DEFAULT '';
