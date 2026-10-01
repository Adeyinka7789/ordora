# Ordora — System Architecture, Database ERD & Engineering Blueprint (V1.0)

**Product:** Ordora  
**Domain:** Order Management & Customer Operations SaaS for SMBs & Service Makers  
**Tech Stack:** Go (Golang) + HTMX + Alpine.js/Tailwind CSS + PostgreSQL  
**Document Type:** Technical System Design & Engineering Blueprint  
**Status:** Approved for Implementation  

---

## 1. Domain Entities & Database ERD (PostgreSQL)

```
┌─────────────────────────────────┐           ┌──────────────────────────────────┐
│          organizations          │1         *│              users               │
├─────────────────────────────────┼───────────┼──────────────────────────────────┤
│ id: uuid (PK)                   │           │ id: uuid (PK)                    │
│ name: varchar(255)              │           │ email: varchar(255) (UNIQUE)     │
│ slug: varchar(100) (UNIQUE)     │           │ password_hash: varchar(255)      │
│ logo_url: text                  │           │ full_name: varchar(255)          │
│ phone: varchar(50)              │           │ role: enum(owner, staff)         │
│ email: varchar(255)             │           │ is_active: boolean               │
│ address: text                   │           │ created_at: timestamptz          │
│ default_currency: char(3)       │           └────────────────┬─────────────────┘
│ public_order_enabled: boolean   │                            │
│ created_at: timestamptz         │                            │ creates / updates
└────────────────┬────────────────┘                            │
                 │ 1                                           │
                 │                                             │
                 ├───┬─────────────────────────────────────────┤
                 │   │                                         │
                 │   ▼ *                                       ▼ *
        ┌────────┴───────────────┐                    ┌────────┴───────────────┐
        │       customers        │1                  *│       audit_logs       │
        ├────────────────────────┼────────────────────┼────────────────────────┤
        │ id: uuid (PK)          │                    │ id: uuid (PK)          │
        │ org_id: uuid (FK)      │                    │ org_id: uuid (FK)      │
        │ full_name: varchar(255)│                    │ user_id: uuid (FK)     │
        │ email: varchar(255)    │                    │ entity_type: varchar   │
        │ phone: varchar(50)     │                    │ entity_id: uuid        │
        │ address: text          │                    │ action: varchar        │
        │ notes: text            │                    │ metadata: jsonb        │
        │ created_at: timestamptz│                    │ created_at: timestamptz│
        └────────┬───────────────┘                    └────────────────────────┘
                 │ 1
                 │
                 ▼ *
┌────────────────────────────────────────────────────────────────────────┐
│                                 orders                                 │
├────────────────────────────────────────────────────────────────────────┤
│ id: uuid (PK)                                                          │
│ org_id: uuid (FK)                                                      │
│ customer_id: uuid (FK)                                                 │
│ order_number: varchar(64) (UNIQUE, e.g. ORD-2026-00128)                │
│ public_token: varchar(64) (UNIQUE secure token for customer portal)    │
│ title: varchar(255) (e.g. '2 Senator Outfits')                         │
│ description: text                                                      │
│ status: enum(NEW, CONFIRMED, IN_PROGRESS, READY, OUT_FOR_DELIVERY,     │
│              DELIVERED, COMPLETED, CANCELLED)                          │
│ subtotal_cents: bigint                                                 │
│ discount_cents: bigint                                                 │
│ tax_cents: bigint                                                      │
│ total_cents: bigint                                                    │
│ amount_paid_cents: bigint                                              │
│ balance_cents: bigint                                                  │
│ currency: char(3) (e.g. NGN, USD, GBP)                                 │
│ payment_status: enum(UNPAID, PARTIALLY_PAID, PAID)                     │
│ expected_delivery_date: date                                           │
│ delivered_at: timestamptz                                              │
│ created_by: uuid (FK users)                                            │
│ notes: text                                                            │
│ created_at: timestamptz                                                │
│ updated_at: timestamptz                                                │
└──────────────────┬─────────────────┬───────────────────┬───────────────┘
                   │ 1               │ 1                 │ 1
                   │                 │                   │
                   ▼ *               ▼ *                 ▼ *
┌────────────────────────┐  ┌────────────────────┐  ┌────────────────────┐
│      order_items       │  │      payments      │  │    attachments     │
├────────────────────────┤  ├────────────────────┤  ├────────────────────┤
│ id: uuid (PK)          │  │ id: uuid (PK)      │  │ id: uuid (PK)      │
│ order_id: uuid (FK)    │  │ order_id: uuid (FK)│  │ org_id: uuid (FK)  │
│ item_name: varchar(255)│  │ org_id: uuid (FK)  │  │ order_id: uuid (FK)│
│ description: text      │  │ amount_cents: bignt│  │ payment_id: uuid   │
│ quantity: integer      │  │ method: enum(bank, │  │ file_name: text    │
│ unit_price_cents: bignt│  │   cash, card, pos) │  │ file_path: text    │
│ total_cents: bigint    │  │ reference: varchar │  │ file_type: varchar │
│ created_at: timestamptz│  │ notes: text        │  │ size_bytes: bigint │
└────────────────────────┘  │ receipt_url: text  │  │ uploaded_by: uuid  │
                            │ recorded_by: uuid  │  │ created_at: tmstmp │
                            │ paid_at: timestmptz│  └────────────────────┘
                            │ created_at: tmstmp │
                            └────────────────────┘
```

---

## 2. Go Backend Modular Project Structure

```
ordora/
├── cmd/
│   ├── server/
│   │   └── main.go                  # Server bootstrap & graceful shutdown
│   └── worker/
│       └── main.go                  # Async background job runner
├── internal/
│   ├── auth/                        # Session cookies, Argon2id password hashing
│   ├── platform/
│   │   ├── database/                # pgx connection pool & migrations
│   │   ├── email/                   # SMTP & Resend driver with templates
│   │   ├── storage/                 # Local filesystem / S3-compatible attachment storage
│   │   └── queue/                   # PostgreSQL transactional outbox job queue
│   ├── domain/
│   │   ├── org/                     # Organization & multi-tenancy models
│   │   ├── customer/                # Customer entity & repository
│   │   ├── order/                   # Order lifecycle state machine & items
│   │   ├── payment/                 # Payment ledger, receipt handling & balance checks
│   │   ├── audit/                   # Security & operational event logging
│   │   └── notification/            # Email triggers & async payloads
│   └── service/                     # Composed business logic services
│       ├── order_service.go         # Status transitions, recalculate balances
│       └── payment_service.go       # ACID transactions: Payment + Order update + Audit
├── web/
│   ├── handlers/                    # HTTP & HTMX handler controllers
│   │   ├── dashboard_handler.go
│   │   ├── order_handler.go         # Full page + partial HTMX fragments
│   │   ├── payment_handler.go
│   │   ├── portal_handler.go        # Public token-authenticated customer view
│   │   └── public_order_handler.go  # Public intake form (ordora.com/order/{slug})
│   ├── middleware/                  # Org multi-tenant isolation, Auth, CSRF, Logger
│   ├── templates/                   # Go html/template components & layouts
│   │   ├── layouts/base.html
│   │   ├── partials/                # HTMX swap fragments (order_row, status_badge, etc.)
│   │   └── pages/                   # Full views
│   └── static/                      # CSS (Tailwind), JS (HTMX 1.9+, Alpine.js)
├── migrations/                      # Flyway/golang-migrate SQL files
└── go.mod
```

---

## 3. Order Status State Machine & Transition Rules

Ordora enforces strict transitions to prevent state corruption:

| From State | Allowed Transitions | Trigger / Action |
|:---|:---|:---|
| **NEW** | CONFIRMED, CANCELLED | Business accepts order, sets completion target |
| **CONFIRMED** | IN_PROGRESS, CANCELLED | Production/service kickoff |
| **IN_PROGRESS** | READY, CANCELLED | Production finished, QA check done; triggers "Order Ready" email |
| **READY** | OUT_FOR_DELIVERY, DELIVERED, COMPLETED | Dispatched or picked up by customer |
| **OUT_FOR_DELIVERY**| DELIVERED, READY (failed delivery) | Courier dropoff confirmed |
| **DELIVERED** | COMPLETED | Final balance collected, customer acceptance |
| **COMPLETED** | *(Terminal)* | Balance must be 0 or authorized write-off |
| **CANCELLED** | *(Terminal)* | Requires audit note |

---

## 4. HTMX Partial Swap Strategy

| Action | HTTP Request | Target `#DOM_ID` | HTMX Strategy | Server Response |
|:---|:---|:---|:---|:---|
| **Change Status** | `POST /orders/:id/status` | `#order-status-panel` | `hx-target="#order-status-panel" hx-swap="outerHTML"` | Renders updated badge, progress stepper, and next status buttons |
| **Live Order Search** | `GET /orders/search?q=...` | `#orders-table-body` | `hx-trigger="keyup changed delay:300ms"` | Renders filtered table rows without full reload |
| **Add Payment** | `POST /orders/:id/payments` | `#payment-summary-card` | `hx-target="#payment-summary-card" hx-swap="outerHTML"` + OOB swap `#orders-row-id` | Inserts payment record, recalculates outstanding balance & badge |
| **Status Filter Tab** | `GET /orders?status=READY` | `#orders-container` | `hx-target="#orders-container" hx-push-url="true"` | Updates URL history and swaps active list |

---

## 5. UI Architecture & Screen Map

1. **Operations Dashboard (`/dashboard`)**: KPI metric cards (Total Orders, ₦ Outstanding, Due Today, Overdue), Quick Status breakdown, and Active Orders table with instant HTMX status filters.
2. **Order Detail & Fulfillment Hub (`/orders/{id}`)**: Live status stepper, order items breakdown, multi-installment payment ledger with receipt modal, customer sidebar info, and audit trail timeline.
3. **Public Customer Order Portal (`/o/{public_token}`)**: Zero-login branded tracking page for customers. Displays real-time progress bar, invoice summary, payment status, delivery date, and downloadable receipts.
4. **Public Order Request Form (`/order/{business_slug}`)**: Self-serve intake form for customers with reference photo upload, dynamic cost estimation, and instant reference code receipt.
