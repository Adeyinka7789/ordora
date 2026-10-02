-- =============================================================
-- 0013_public_token_meta.sql
--
-- Adds metadata to orders for public portal token lifecycle:
--   - last_accessed_at: bumped on every successful portal read
--   - revoked_at:       set when a token is invalidated
-- =============================================================

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS public_token_last_accessed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS public_token_revoked_at       TIMESTAMPTZ;

-- Partial index for fast token lookups, excluding revoked tokens.
CREATE INDEX IF NOT EXISTS idx_orders_public_token_active
    ON orders (public_token_hash)
    WHERE public_token_hash IS NOT NULL
      AND public_token_revoked_at IS NULL;
