-- =============================================================
-- 0025_search_indexes.sql
--
-- Trigram indexes to make global search fast on orders,
-- customers, and products.
-- =============================================================

-- Orders: order_number, title
CREATE INDEX IF NOT EXISTS idx_orders_number_trgm
    ON orders USING gin (order_number gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_orders_title_trgm
    ON orders USING gin (title gin_trgm_ops);

-- Customers: phone (name/email already have trigram from earlier)
CREATE INDEX IF NOT EXISTS idx_customers_phone_trgm
    ON customers USING gin (phone gin_trgm_ops);

-- Products: sku (name already has trigram from earlier)
CREATE INDEX IF NOT EXISTS idx_products_sku_trgm
    ON products USING gin (sku gin_trgm_ops);
