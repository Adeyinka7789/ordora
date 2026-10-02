-- =============================================================
-- 0001_init.sql
-- Creates every V1 table for Ordora.
-- Runs as: ordora_admin
-- =============================================================

-- ---------- Enums ----------
CREATE TYPE order_status AS ENUM (
    'NEW','CONFIRMED','IN_PROGRESS','READY','OUT_FOR_DELIVERY','DELIVERED','COMPLETED','CANCELLED'
);

CREATE TYPE payment_method AS ENUM (
    'CASH','BANK_TRANSFER','CARD','POS','OTHER'
);

-- ---------- Organizations (tenants) ----------
CREATE TABLE organizations (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    slug        CITEXT NOT NULL UNIQUE,
    logo_key    TEXT,
    email       CITEXT,
    phone       TEXT,
    address     TEXT,
    currency    CHAR(3) NOT NULL DEFAULT 'NGN',
    timezone    TEXT NOT NULL DEFAULT 'Africa/Lagos',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- Users ----------
CREATE TABLE users (
    id                UUID PRIMARY KEY,
    email             CITEXT NOT NULL UNIQUE,
    password_hash     TEXT NOT NULL,
    name              TEXT NOT NULL,
    email_verified_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- Organization Members ----------
CREATE TABLE organization_members (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('OWNER','ADMIN','MANAGER','STAFF','ACCOUNTANT','VIEWER')),
    status          TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INVITED','DISABLED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX idx_org_members_user ON organization_members(user_id);

-- ---------- Sessions ----------
CREATE TABLE sessions (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      BYTEA NOT NULL UNIQUE,
    organization_id UUID REFERENCES organizations(id),
    user_agent      TEXT,
    ip              INET,
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_user ON sessions(user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_sessions_expires ON sessions(expires_at) WHERE revoked_at IS NULL;

-- ---------- Auth tokens (email verify + password reset) ----------
CREATE TABLE auth_tokens (
    id           UUID PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('EMAIL_VERIFY','PASSWORD_RESET')),
    token_hash   BYTEA NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_tokens_user ON auth_tokens(user_id, kind) WHERE consumed_at IS NULL;

-- ---------- Customers ----------
CREATE TABLE customers (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    email           CITEXT,
    phone           TEXT,
    address         TEXT,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_customers_org        ON customers(organization_id);
CREATE INDEX idx_customers_org_name   ON customers(organization_id, name);
CREATE INDEX idx_customers_org_phone  ON customers(organization_id, phone);

-- ---------- Orders ----------
CREATE TABLE orders (
    id                  UUID PRIMARY KEY,
    organization_id     UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    customer_id         UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    order_number        TEXT NOT NULL,
    public_token_hash   BYTEA,
    title               TEXT NOT NULL,
    description         TEXT,
    status              order_status NOT NULL DEFAULT 'NEW',
    currency            CHAR(3) NOT NULL,
    subtotal_minor      BIGINT NOT NULL DEFAULT 0 CHECK (subtotal_minor  >= 0),
    discount_minor      BIGINT NOT NULL DEFAULT 0 CHECK (discount_minor  >= 0),
    tax_minor           BIGINT NOT NULL DEFAULT 0 CHECK (tax_minor       >= 0),
    total_minor         BIGINT NOT NULL DEFAULT 0 CHECK (total_minor     >= 0),
    amount_paid_minor   BIGINT NOT NULL DEFAULT 0 CHECK (amount_paid_minor >= 0),
    expected_completion DATE,
    delivered_at        TIMESTAMPTZ,
    created_by          UUID NOT NULL REFERENCES users(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, order_number)
);
CREATE INDEX idx_orders_org_status  ON orders(organization_id, status);
CREATE INDEX idx_orders_org_due     ON orders(organization_id, expected_completion);
CREATE INDEX idx_orders_customer    ON orders(customer_id);
CREATE UNIQUE INDEX idx_orders_public_token ON orders(public_token_hash) WHERE public_token_hash IS NOT NULL;

-- ---------- Order items ----------
CREATE TABLE order_items (
    id               UUID PRIMARY KEY,
    organization_id  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    order_id         UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    description      TEXT NOT NULL,
    quantity         NUMERIC(12,3) NOT NULL CHECK (quantity > 0),
    unit_price_minor BIGINT NOT NULL CHECK (unit_price_minor >= 0),
    subtotal_minor   BIGINT NOT NULL CHECK (subtotal_minor   >= 0),
    position         INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_order_items_order ON order_items(order_id);

-- ---------- Payments ----------
CREATE TABLE payments (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    amount_minor    BIGINT NOT NULL CHECK (amount_minor > 0),
    currency        CHAR(3) NOT NULL,
    method          payment_method NOT NULL,
    reference       TEXT,
    paid_at         TIMESTAMPTZ NOT NULL,
    notes           TEXT,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    reversed_by     UUID REFERENCES payments(id),
    reverses        UUID REFERENCES payments(id)
);
CREATE INDEX idx_payments_order      ON payments(order_id);
CREATE INDEX idx_payments_org_paid_at ON payments(organization_id, paid_at);

-- ---------- Attachments ----------
CREATE TABLE attachments (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     TEXT NOT NULL CHECK (entity_type IN ('ORDER','PAYMENT','CUSTOMER')),
    entity_id       UUID NOT NULL,
    storage_key     TEXT NOT NULL,
    filename        TEXT NOT NULL,
    mime_type       TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL CHECK (size_bytes > 0),
    uploaded_by     UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_attachments_entity ON attachments(organization_id, entity_type, entity_id);

-- ---------- Outbox ----------
CREATE TABLE outbox (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    event_name      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    dispatched_at   TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT
);
CREATE INDEX idx_outbox_pending ON outbox(occurred_at) WHERE dispatched_at IS NULL;

-- ---------- Notification jobs ----------
CREATE TABLE notification_jobs (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    kind            TEXT NOT NULL,
    channel         TEXT NOT NULL DEFAULT 'EMAIL',
    recipient       CITEXT NOT NULL,
    payload         JSONB NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    status          TEXT NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING','SENT','FAILED','DEAD')),
    attempts        INT NOT NULL DEFAULT 0,
    last_error      TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX idx_notif_pending ON notification_jobs(next_attempt_at) WHERE status = 'PENDING';

-- ---------- Audit logs ----------
CREATE TABLE audit_logs (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL,
    actor_user_id   UUID REFERENCES users(id),
    action          TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    before          JSONB,
    after           JSONB,
    ip              INET,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_org_created ON audit_logs(organization_id, created_at DESC);
CREATE INDEX idx_audit_entity      ON audit_logs(organization_id, entity_type, entity_id);
