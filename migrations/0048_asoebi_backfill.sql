-- =============================================================
-- 0048_asoebi_backfill.sql
--
-- Old groups (pre-0047) have NULL join_slug, so /g/ links 404 with the
-- generic order-tracking message. Backfill a stable public slug so
-- every existing group instantly gets a working join link.
-- Manage keys stay NULL until the tailor next opens the group page
-- (EnsureTokens mints them then; the raw key is shown once).
-- =============================================================

UPDATE order_groups
SET join_slug = substring(md5(id::text), 1, 16),
    updated_at = now()
WHERE join_slug IS NULL OR join_slug = '';

-- Safety: if 0047 was partially applied before, make sure the TEXT
-- join functions exist (idempotent re-create is harmless).
-- (Bodies duplicated from 0047; kept here so a fresh DB from scratch
--  and an upgraded DB converge.)
