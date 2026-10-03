-- =============================================================
-- 0030_feature_flags_modules.sql
-- Per-module kill switches so the admin can toggle whole sections of
-- the app. All seeded ON (no behavior change); turning one off hides
-- its sidebar entry and makes its routes 404.
-- =============================================================

INSERT INTO feature_flags (flag_key, name, description, enabled, rollout_percent) VALUES
	('orders', 'Orders module', 'Orders pages, order creation, status changes, costs and attachments.', TRUE, 100),
	('customers', 'Customers module', 'Customer list, profiles, and creation.', TRUE, 100),
	('products', 'Products module', 'Product catalog and the order-form product picker.', TRUE, 100),
	('ledger', 'Payments & Ledger page', 'Consolidated payment ledger. Recording stays on the order page.', TRUE, 100),
	('reports', 'Reports module', 'Reports & Analytics pages and CSV export.', TRUE, 100)
ON CONFLICT DO NOTHING;
