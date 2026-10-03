-- =============================================================
-- 0031_session_last_seen.sql
-- Tracks per-session activity for idle timeout enforcement.
-- last_seen_at starts at now() for existing rows (all sessions become
-- "recently active"; idle expiry applies going forward).
-- =============================================================

ALTER TABLE sessions
	ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now();
