# Ordora — Order Management SaaS

Ordora is order management for businesses that take custom orders —
tailors, bakers, printers, furniture makers, repair shops. Track every
order, record split payments, share live customer tracking links, and run
reports — all multi-tenant, all self-hosted.

Server-rendered Go + HTMX + Tailwind. No SPA, no public API.

## Stack

- **Go 1.22+**, stdlib `net/http` router (1.22+ method+path patterns), HTMX fragments
- **PostgreSQL 16** with Row-Level Security per tenant, transactional outbox
- Background worker (outbox relay + email notifications), console or SMTP mail
- Sentry for error monitoring (optional, DSN-gated)

## Quick start

Prerequisites: Go 1.22+, PostgreSQL 16, `psql` on PATH. Full database
setup (roles, extensions, grants) is in [`SETUP.md`](SETUP.md).

```powershell
# 1. Configure
Copy-Item .env.example .env   # then edit secrets

# 2. Migrate
go run ./cmd/migrate

# 3. Create the platform admin
go run ./cmd/seedadmin --email you@yourdomain.com

# 4. Run (dev runs the background workers in-process)
go run ./cmd/server
```

Open http://localhost:8080. Register a business, or sign in. The admin
panel lives at the secret path in `ORDORA_ADMIN_PATH` (default
`/ops-x9k2m` + `/login`).

## Everyday commands

| Command | Purpose |
|---|---|
| `go run ./cmd/server` | Web app (dev also runs workers in-process) |
| `go run ./cmd/worker` | Background workers (production: run separately, e.g. systemd) |
| `go run ./cmd/migrate` | Apply pending `migrations/*.sql` (idempotent) |
| `go run ./cmd/seedadmin --email X` | Create/reset a platform admin (`--reset-password` to rotate) |
| `go build ./...` / `go vet ./...` | Compile + vet |
| `go test ./...` | Unit + DB-backed tests (needs the `ORDORA_DB_*` env, skips without) |
| `scripts/check.ps1` | Repo health check |

## Configuration

All config comes from environment (see `.env.example`). Nothing reads
`os.Getenv` outside `internal/config`.

| Variable | Default | Purpose |
|---|---|---|
| `ORDORA_ENV` | `development` | `development` or `production` (JSON logs, secure cookies in prod) |
| `ORDORA_HTTP_ADDR` | `:8080` | Listen address |
| `ORDORA_BASE_URL` | `http://localhost:8080` | Links in emails |
| `ORDORA_DB_HOST/PORT/NAME/USER/PASSWORD/SSLMODE` | — | App role (RLS-enforced) |
| `ORDORA_DB_ADMIN_USER/PASSWORD` | — | Migration + admin-panel role (`BYPASSRLS`) |
| `ORDORA_SESSION_COOKIE_NAME` | `ordora_session` | Business session cookie |
| `ORDORA_SESSION_TTL_HOURS` | `720` | Absolute session lifetime (30 days) |
| `ORDORA_SESSION_IDLE_HOURS` | `168` | Idle cap (7 days); `0` disables, TTL still applies |
| `ORDORA_CSRF_COOKIE_NAME` | `ordora_csrf` | CSRF cookie |
| `ORDORA_EMAIL_MODE` | `console` | `console` (stdout) or `smtp` |
| `ORDORA_EMAIL_FROM` | — | From address |
| `ORDORA_SMTP_HOST/PORT/USERNAME/PASSWORD` | — | Only when email mode is `smtp` |
| `ORDORA_STORAGE_MODE` | `local` | `local` or `s3` (attachments) |
| `ORDORA_SUPPORT_EMAIL` | `support@ordora.local` | Footer contact + support reference |
| `ORDORA_SENTRY_DSN` | _(empty = disabled)_ | Sentry project DSN |
| `ORDORA_SENTRY_ENVIRONMENT` | = `ORDORA_ENV` | Sentry environment tag |
| `ORDORA_SENTRY_TRACES_SAMPLE_RATE` | `0` | Fraction of requests traced (`1.0` = all; keep low in prod) |
| `ORDORA_SENTRY_ENABLE_LOGS` | `true` | Forward slog Warn/Error records to Sentry Logs |
| `ORDORA_SENTRY_VERIFY` | _(off)_ | Set `1` for a one-shot "It works!" ping at startup |
| `ORDORA_ADMIN_PATH` | `/ops-x9k2m` | Secret admin panel prefix — change in production |
| `ORDORA_ADMIN_SESSION_COOKIE` | `ordora_admin_session` | Must differ from business cookie |
| `ORDORA_ADMIN_SESSION_TTL_HOURS` | `8` | Admin session lifetime |

## Architecture

```
browser ──▶ middleware (recover → request-id → logger → session → CSRF)
         ──▶ handlers (parse input, call services, render HTML/HTMX)
         ──▶ app services (transactions, workflows, audit, outbox)
         ──▶ domain (pure rules: order totals, payment methods, slugs…)
         ──▶ infra/postgres (RLS-scoped repos) / email / storage
```

- **Multi-tenancy**: every tenant table has `organization_id` + RLS
  policies reading `app.current_org_id`, set per transaction by
  `DB.WithTenant`. The admin pool connects as `ordora_admin`
  (`BYPASSRLS`) for cross-tenant reads. Tables without RLS (`users`,
  `sessions`, `feature_flags`) are safe by construction: keyed by user
  id or platform-global, never tenant-mixed.
- **Outbox**: domain events (`order.created`, `payment.recorded`, …)
  are written transactionally, then relayed by the worker into
  `notification_jobs` and emailed. Never send email inside a request.
- **Money**: integer minor units everywhere; display via the `currency`
  template func (`CODE 1,234,567.89`, thousands grouped). CSV exports
  stay raw minors for spreadsheets.
- **Sessions**: SHA-256-hashed random tokens, HttpOnly + Secure (prod)
  + SameSite cookies; 30-day absolute TTL + 7-day idle cap (both
  configurable); device + IP recorded at login; visible per-device
  revoke on the profile page; password change signs out other devices;
  password reset signs out everywhere.
- **Feature flags** (Waffle-style, `internal/flags`): DB-backed kill
  switches + deterministic per-org % rollouts, toggled in Admin →
  Flags (audited, fail-closed). Gate Go code with
  `provider.Enabled(orgID, "key")`; gate layout templates with
  `{{ if flag "key" .Shell.OrgID }}` (page fragments only get page
  data — precompute a bool there instead). Current flags: `orders`,
  `customers`, `products`, `ledger`, `ledger_export`, `reports`,
  `support_desk`.

## Admin panel guide

Log in at `{ADMIN_PATH}/login` (seeded via `cmd/seedadmin`). Sections:

- **Tenants / Users** — search, inspect, suspend/unsuspend/delete orgs,
  force-logout users, impersonate an org (30-min audited sessions).
- **Complaints** — user-filed support requests from `/support`. Reply
  (user gets an in-app notification), resolve/reopen.
- **Broadcasts** — one message fanned out to every tenant's
  notification feed. In-app only, no email blast.
- **Flags** — feature switches + rollouts (see above).
- **Data / Lookup / Ops / Audit** — cross-tenant table browser, order
  and notification-job lookup, platform health, full audit trail.

Every admin action writes an audit row with admin id, IP, and metadata.

## Operations

- **Processes**: `cmd/server` + `cmd/worker` (systemd units or similar;
  dev runs workers in-process). Both flush Sentry on shutdown.
- **Errors**: Sentry DSN on the VPS; panics (HTTP + background) report
  with request id / worker tag. Without a DSN, rely on stdout logs.
- **Backups**: automate `pg_dump` of the database (not included —
  do this before real tenants arrive).
- **Email**: start with a local relay or transactional provider over
  SMTP; verify SPF/DKIM for the sending domain.
- **Migrations** run forward-only via `cmd/migrate`; new tables need
  RLS policies unless platform-global (document why, like above).

## Testing

- `go test ./...` — domain unit tests, service tests with fakes,
  template parse + fragment-execution regression tests (these catch
  template/data mismatches `go build` can't see), and DB-backed repo
  tests (skipped without `ORDORA_DB_*` env).
- New repo method? Add a DB-backed test following
  `attachment_repo_test.go`. New template? Extend
  `fragments_render_test.go`.

## Project structure

```
cmd/server|worker|migrate|seedadmin  binaries
internal/app        services (workflows, transactions)
internal/auth       login/register/sessions/passwords, admin auth
internal/domain     pure business rules (order, payment, money, …)
internal/flags      feature-flag evaluation (cached, fail-closed)
internal/infra      postgres repos, email, storage, ids
internal/jobs       outbox relay + notification worker
internal/observe    Sentry init/report/flush, SafeGo
internal/web        handlers, middleware, templates, render
migrations          forward-only SQL (numbered)
```
