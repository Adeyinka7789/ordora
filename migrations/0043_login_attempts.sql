-- =============================================================
-- 0043_login_attempts.sql
--
-- Brute-force protection for business login: counts consecutive
-- failures per email (normalized lowercase) and locks the account
-- for 15 minutes after 5 failures. Rows are keyed by email even
-- for addresses with no account, so locked/unknown responses stay
-- uniform and don't leak whether an email is registered.
--
-- Deliberately NO RLS: like users/sessions, this is a platform-level
-- table keyed by login identity, never tenant-mixed. Writes happen
-- only in the auth service, never from tenant request paths.
-- =============================================================

CREATE TABLE login_attempts (
    email        CITEXT PRIMARY KEY,
    failures     INT NOT NULL DEFAULT 0 CHECK (failures >= 0),
    locked_until TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO admin_browsable_tables (table_name, display_name, search_columns, order_by, order_dir) VALUES
    ('login_attempts', 'Login Attempts', ARRAY['email'], 'updated_at', 'DESC')
ON CONFLICT (table_name) DO NOTHING;
