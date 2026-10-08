-- =============================================================
-- 0051_admin_browser_hardening.sql
--
-- Removes session/token tables from the admin data-browser whitelist.
-- Browsing sessions, admin_sessions, impersonation_sessions, or
-- auth_tokens exposed token hashes, IPs, and user agents to platform
-- admins. The Go layer (AdminDataRepo deniedTables + isDeniedColumn)
-- blocks them regardless; this keeps fresh databases clean too.
-- =============================================================

DELETE FROM admin_browsable_tables
WHERE table_name IN (
    'sessions',
    'admin_sessions',
    'impersonation_sessions',
    'auth_tokens'
);
