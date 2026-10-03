-- =============================================================
-- 0026_reporting_indexes.sql
--
-- Indexes that make the reporting aggregates fast.
-- Mostly composite (organization_id, <date>) since every report
-- runs under a tenant and filters by a date range.
-- =============================================================

-- Orders by created_at (for sales trend).
CREATE INDEX IF NOT EXISTS idx_orders_org_created
    ON orders (organization_id, created_at DESC);

-- Payments by paid_at (for cash flow).
CREATE INDEX IF NOT EXISTS idx_payments_org_paid_at_desc
    ON payments (organization_id, paid_at DESC)
    WHERE reversed_by IS NULL AND reverses IS NULL;

-- Payments by method (for channel breakdown).
CREATE INDEX IF NOT EXISTS idx_payments_org_method
    ON payments (organization_id, method)
    WHERE reversed_by IS NULL AND reverses IS NULL;

-- Order items joined to orders for category reports.
CREATE INDEX IF NOT EXISTS idx_order_items_org
    ON order_items (organization_id);
