-- =============================================================
-- 0009_customer_index.sql
--
-- Adds a trigram index on customers.name so that ILIKE '%foo%'
-- search stays fast. Also adds an index for email lookup.
--
-- pg_trgm is installed by the host (superuser), not by the app
-- role. See SETUP.md for the one-time step.
-- =============================================================

CREATE INDEX IF NOT EXISTS idx_customers_org_name_trgm
    ON customers USING gin (name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_customers_org_email
    ON customers (organization_id, email)
    WHERE email IS NOT NULL AND email <> '';