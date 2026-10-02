-- =============================================================
-- 0019_notification_kinds.sql
--
-- Adds an index for the worker's claim query. The notification_jobs
-- table exists from migration 0001; this just makes the polling
-- efficient under load.
-- =============================================================

-- Idempotent: drop and recreate.
DROP INDEX IF EXISTS idx_notif_claim;

CREATE INDEX idx_notif_claim
    ON notification_jobs (next_attempt_at, id)
    WHERE status = 'PENDING';
