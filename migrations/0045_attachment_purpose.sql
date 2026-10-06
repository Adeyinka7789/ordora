-- =============================================================
-- 0045_attachment_purpose.sql
--
-- Style reference photos: attachments gain a purpose so inspiration
-- images render first-class on the order page instead of hiding in
-- the generic file list. 'general' is the default for everything
-- uploaded before this migration (and for payment proofs).
-- Purposes are validated in app code to allow new values without
-- a migration.
-- =============================================================

ALTER TABLE attachments
    ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'general';
