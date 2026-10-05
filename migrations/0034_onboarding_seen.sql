-- =============================================================
-- 0034_onboarding_seen.sql
--
-- First-run onboarding wizard memory: NULL means the user has never
-- finished or skipped the wizard and should see it after login.
-- =============================================================

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding_completed_at TIMESTAMPTZ;
