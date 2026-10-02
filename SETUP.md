# Ordora — Local Development Setup

## Prerequisites

- Go 1.22+
- PostgreSQL 16 (Windows native install)
- `psql` on PATH
- Git

## One-time database setup

As the `postgres` superuser:

```sql
CREATE DATABASE ordora_dev;

\c ordora_dev
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE ROLE ordora_admin WITH LOGIN PASSWORD '<strong-password>';
CREATE ROLE ordora_app   WITH LOGIN PASSWORD '<strong-password>';

ALTER ROLE ordora_app   NOBYPASSRLS;
ALTER ROLE ordora_admin BYPASSRLS;   -- needed for SECURITY DEFINER fns

\c ordora_dev

GRANT CONNECT ON DATABASE ordora_dev TO ordora_app, ordora_admin;
GRANT USAGE ON SCHEMA public TO ordora_app, ordora_admin;
ALTER SCHEMA public OWNER TO ordora_admin;

ALTER DEFAULT PRIVILEGES FOR ROLE ordora_admin IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ordora_app;
ALTER DEFAULT PRIVILEGES FOR ROLE ordora_admin IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO ordora_app;
```

## Saving Files — Don't Trust the Editor

When VS Code saves a file, it writes to disk and the buffer is clean. But **if disk is full, the save silently fails** in some cases.

**Verification after every important edit:**

```powershell
Select-String -Path <file> -Pattern "the-change-you-made"
```

### Extensions (superuser only)

The following extensions must be installed by the `postgres` superuser **before** running migrations:

```sql
\c ordora_dev
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```
