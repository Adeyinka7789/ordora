-- =============================================================
-- 0023_admin_browsable_tables.sql
--
-- Defines which tables the admin data browser can read, and what
-- columns to show/search. This is a security boundary: no admin
-- query ever touches a table not in this list.
-- =============================================================

CREATE TABLE admin_browsable_tables (
    table_name      TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    search_columns  TEXT[] NOT NULL DEFAULT '{}', -- ILIKE-able text columns
    order_by        TEXT NOT NULL DEFAULT 'created_at',
    order_dir       TEXT NOT NULL DEFAULT 'DESC' CHECK (order_dir IN ('ASC','DESC')),
    max_limit       INT NOT NULL DEFAULT 100,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed the whitelist.
INSERT INTO admin_browsable_tables (table_name, display_name, search_columns, order_by, order_dir) VALUES
    ('organizations',        'Organizations',      ARRAY['name','slug','email'],        'created_at', 'DESC'),
    ('users',                'Users',              ARRAY['email','name'],               'created_at', 'DESC'),
    ('organization_members', 'Organization Members', ARRAY['role','status'],            'created_at', 'DESC'),
    ('customers',            'Customers',          ARRAY['name','email','phone'],       'created_at', 'DESC'),
    ('orders',               'Orders',             ARRAY['order_number','title'],       'created_at', 'DESC'),
    ('order_items',          'Order Items',        ARRAY['description'],                'created_at', 'DESC'),
    ('payments',             'Payments',           ARRAY['reference','notes'],          'created_at', 'DESC'),
    ('attachments',          'Attachments',        ARRAY['filename','mime_type'],       'created_at', 'DESC'),
    ('audit_logs',           'Audit Logs',         ARRAY['action','entity_type'],       'created_at', 'DESC'),
    ('admin_audit_logs',     'Admin Audit Logs',   ARRAY['action','target_type'],       'created_at', 'DESC'),
    ('outbox',               'Outbox Events',      ARRAY['event_name'],                 'occurred_at','DESC'),
    ('notification_jobs',    'Notification Jobs',  ARRAY['kind','recipient','status'],  'created_at', 'DESC'),
    ('sessions',             'Sessions',           ARRAY['user_agent'],                 'created_at', 'DESC'),
    ('admin_sessions',       'Admin Sessions',     ARRAY['user_agent'],                 'created_at', 'DESC'),
    ('impersonation_sessions','Impersonation Sessions', ARRAY[]::text[],                'created_at', 'DESC'),
    ('order_counters',       'Order Counters',     ARRAY[]::text[],                     'updated_at', 'DESC'),
    ('products',             'Products',           ARRAY['name','sku'],                 'created_at', 'DESC'),
    ('schema_migrations',    'Schema Migrations',  ARRAY['version'],                    'applied_at', 'DESC')
ON CONFLICT (table_name) DO NOTHING;
