package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// ReportRepo contains all reporting aggregate queries.
type ReportRepo struct {
	db *DB
}

func NewReportRepo(db *DB) *ReportRepo { return &ReportRepo{db: db} }

// ---- Types ----

// KPIs holds the four hero metrics.
type KPIs struct {
	GrossInvoicedMinor int64
	CashSettledMinor   int64
	OutstandingMinor   int64
	BookedOrders       int64
	CompletedOrders    int64
	AvgTicketMinor     int64
	RepeatCustomerPct  int
	TotalCustomers     int64
	RepeatCustomers    int64
	CollectionRatePct  int
	VelocityDays       float64 // avg days from NEW to DELIVERED
	PrevGrossMinor     int64   // previous period, for % change
}

// TrendPoint is one bucket on the sales trend chart.
type TrendPoint struct {
	BucketStart    time.Time
	InvoicedMinor  int64
	SettledMinor   int64
	MilestoneMinor int64 // outstanding within the bucket
}

// CategoryRow is one product/service category.
type CategoryRow struct {
	Description    string
	Orders         int64
	TotalMinor     int64
	AvgTicketMinor int64
	PercentOfTotal float64
}

// ChannelRow is one payment method.
type ChannelRow struct {
	Method     string
	TotalMinor int64
	Count      int64
	Percent    float64
}

// StageDuration is a fulfillment stage timing.
type StageDuration struct {
	Stage    string
	AvgHours float64
	Longest  bool // is this the current bottleneck?
}

// TopCustomer is one customer ranked by revenue.
type TopCustomer struct {
	ID          uuid.UUID
	Name        string
	Email       string
	Orders      int64
	TotalMinor  int64
	LastOrderAt time.Time
}

// Insight is a rule-generated alert.
type Insight struct {
	Severity   string // "info" | "warn" | "critical"
	Title      string
	Message    string
	ActionURL  string
	ActionText string
}

// ---- Queries ----

// KPIs returns the four hero metrics for the given range.
// KPIs returns the four hero metrics for the given range.
func (r *ReportRepo) KPIs(ctx context.Context, scope tenant.TenantScope, from, to time.Time) (*KPIs, error) {
	var k KPIs
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Base counts + sums.
		const q = `
			SELECT
				COALESCE(SUM(total_minor) FILTER (WHERE status <> 'CANCELLED'), 0) AS gross,
				COUNT(*) FILTER (WHERE status <> 'CANCELLED') AS booked,
				COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed,
				COUNT(DISTINCT customer_id) AS total_customers
			FROM orders
			WHERE organization_id = $1
			  AND created_at >= $2
			  AND created_at < $3
		`
		if err := tx.QueryRow(ctx, q, scope.OrgID, from, to).Scan(
			&k.GrossInvoicedMinor, &k.BookedOrders, &k.CompletedOrders, &k.TotalCustomers,
		); err != nil {
			return fmt.Errorf("report_repo: kpis orders: %w", Classify(err))
		}

		// Cash settled in the period.
		const settledQ = `
			SELECT COALESCE(SUM(amount_minor), 0)
			FROM payments
			WHERE organization_id = $1
			  AND paid_at >= $2
			  AND paid_at < $3
			  AND reversed_by IS NULL
			  AND reverses IS NULL
		`
		if err := tx.QueryRow(ctx, settledQ, scope.OrgID, from, to).Scan(&k.CashSettledMinor); err != nil {
			return err
		}

		// Outstanding: total - paid on non-cancelled orders.
		const outQ = `
			SELECT COALESCE(SUM(total_minor - amount_paid_minor), 0)
			FROM orders
			WHERE organization_id = $1
			  AND status <> 'CANCELLED'
		`
		if err := tx.QueryRow(ctx, outQ, scope.OrgID).Scan(&k.OutstandingMinor); err != nil {
			return err
		}

		// Previous period gross.
		dur := to.Sub(from)
		prevFrom := from.Add(-dur)
		const prevQ = `
			SELECT COALESCE(SUM(total_minor), 0)
			FROM orders
			WHERE organization_id = $1
			  AND created_at >= $2
			  AND created_at < $3
			  AND status <> 'CANCELLED'
		`
		if err := tx.QueryRow(ctx, prevQ, scope.OrgID, prevFrom, from).Scan(&k.PrevGrossMinor); err != nil {
			return err
		}

		// Average ticket.
		if k.BookedOrders > 0 {
			k.AvgTicketMinor = k.GrossInvoicedMinor / k.BookedOrders
		}

		// Collection rate.
		if k.GrossInvoicedMinor > 0 {
			k.CollectionRatePct = int(k.CashSettledMinor * 100 / k.GrossInvoicedMinor)
		}

		// Velocity.
		const velQ = `
			SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (delivered_at - created_at)) / 86400.0), 0)
			FROM orders
			WHERE organization_id = $1
			  AND delivered_at IS NOT NULL
			  AND created_at >= $2
			  AND created_at < $3
		`
		if err := tx.QueryRow(ctx, velQ, scope.OrgID, from, to).Scan(&k.VelocityDays); err != nil {
			return err
		}

		// Repeat customers.
		const repQ = `
			WITH c AS (
				SELECT customer_id, COUNT(*) AS n
				FROM orders
				WHERE organization_id = $1
				  AND created_at >= $2
				  AND created_at < $3
				  AND status <> 'CANCELLED'
				GROUP BY customer_id
			)
			SELECT
				COALESCE(COUNT(*) FILTER (WHERE n >= 2), 0) AS repeat,
				COALESCE(COUNT(*), 0) AS total
			FROM c
		`
		var totalCust int64
		if err := tx.QueryRow(ctx, repQ, scope.OrgID, from, to).Scan(&k.RepeatCustomers, &totalCust); err != nil {
			return err
		}
		if totalCust > 0 {
			k.RepeatCustomerPct = int(k.RepeatCustomers * 100 / totalCust)
		}

		return nil
	})
	return &k, err
}

// Trend returns daily/weekly buckets between from and to.
// Trend returns daily/weekly buckets between from and to.
func (r *ReportRepo) Trend(ctx context.Context, scope tenant.TenantScope, from, to time.Time, bucketDays int) ([]TrendPoint, error) {
	if bucketDays <= 0 {
		bucketDays = 7
	}
	var out []TrendPoint
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			WITH buckets AS (
				SELECT generate_series(
					date_trunc('day', $2::timestamptz),
					date_trunc('day', $3::timestamptz),
					($4::int || ' days')::interval
				) AS bucket_start
			)
			SELECT
				b.bucket_start,
				COALESCE(SUM(o.total_minor) FILTER (WHERE o.status <> 'CANCELLED'), 0) AS invoiced,
				COALESCE(SUM(o.amount_paid_minor), 0) AS settled
			FROM buckets b
			LEFT JOIN orders o
			  ON o.organization_id = $1
			 AND o.created_at >= b.bucket_start
			 AND o.created_at < b.bucket_start + ($4::int || ' days')::interval
			GROUP BY b.bucket_start
			ORDER BY b.bucket_start
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to, bucketDays)
		if err != nil {
			return fmt.Errorf("report_repo: trend: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var p TrendPoint
			if err := rows.Scan(&p.BucketStart, &p.InvoicedMinor, &p.SettledMinor); err != nil {
				return err
			}
			p.MilestoneMinor = p.InvoicedMinor - p.SettledMinor
			if p.MilestoneMinor < 0 {
				p.MilestoneMinor = 0
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// Categories returns top product categories (from order_items descriptions).
func (r *ReportRepo) Categories(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]CategoryRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var out []CategoryRow
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Group items by first N chars of description to approximate "categories".
		// Better: group by product_id once products are linked to items. For now, group by description.
		const q = `
			SELECT
				oi.description,
				COUNT(DISTINCT oi.order_id) AS orders,
				COALESCE(SUM(oi.subtotal_minor), 0) AS total_minor
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id
			WHERE oi.organization_id = $1
			  AND o.created_at >= $2
			  AND o.created_at < $3
			  AND o.status <> 'CANCELLED'
			GROUP BY oi.description
			ORDER BY total_minor DESC
			LIMIT $4
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to, limit)
		if err != nil {
			return fmt.Errorf("report_repo: categories: %w", Classify(err))
		}
		defer rows.Close()

		var grand int64
		for rows.Next() {
			var c CategoryRow
			if err := rows.Scan(&c.Description, &c.Orders, &c.TotalMinor); err != nil {
				return err
			}
			grand += c.TotalMinor
			if c.Orders > 0 {
				c.AvgTicketMinor = c.TotalMinor / c.Orders
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if grand > 0 {
			for i := range out {
				out[i].PercentOfTotal = float64(out[i].TotalMinor) * 100 / float64(grand)
			}
		}
		return nil
	})
	return out, err
}

// Channels returns payment method distribution.
func (r *ReportRepo) Channels(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]ChannelRow, error) {
	var out []ChannelRow
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT method::text, COALESCE(SUM(amount_minor), 0), COUNT(*)
			FROM payments
			WHERE organization_id = $1
			  AND paid_at >= $2
			  AND paid_at < $3
			  AND reversed_by IS NULL AND reverses IS NULL
			GROUP BY method
			ORDER BY 2 DESC
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to)
		if err != nil {
			return fmt.Errorf("report_repo: channels: %w", Classify(err))
		}
		defer rows.Close()
		var grand int64
		for rows.Next() {
			var c ChannelRow
			if err := rows.Scan(&c.Method, &c.TotalMinor, &c.Count); err != nil {
				return err
			}
			grand += c.TotalMinor
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if grand > 0 {
			for i := range out {
				out[i].Percent = float64(out[i].TotalMinor) * 100 / float64(grand)
			}
		}
		return nil
	})
	return out, err
}

// StageDurations returns average time in each fulfillment stage.
//
// We use audit_logs to reconstruct when each transition happened.
func (r *ReportRepo) StageDurations(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]StageDuration, error) {
	var out []StageDuration
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// For each order, compute duration between consecutive status changes.
		// Simplify: compute the average duration for each "from_status" transition.
		const q = `
			WITH transitions AS (
				SELECT
					al.entity_id AS order_id,
					(al.after->>'status') AS new_status,
					al.created_at,
					LAG(al.created_at) OVER (PARTITION BY al.entity_id ORDER BY al.created_at) AS prev_at
				FROM audit_logs al
				WHERE al.organization_id = $1
				  AND al.action = 'order.status_changed'
				  AND al.created_at >= $2
				  AND al.created_at < $3
			)
			SELECT
				new_status,
				AVG(EXTRACT(EPOCH FROM (created_at - prev_at)) / 3600.0) AS avg_hours
			FROM transitions
			WHERE prev_at IS NOT NULL
			GROUP BY new_status
			ORDER BY avg_hours DESC
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to)
		if err != nil {
			return fmt.Errorf("report_repo: stages: %w", Classify(err))
		}
		defer rows.Close()
		i := 0
		for rows.Next() {
			var s StageDuration
			var avgHours *float64
			if err := rows.Scan(&s.Stage, &avgHours); err != nil {
				return err
			}
			if avgHours != nil {
				s.AvgHours = *avgHours
			}
			s.Longest = i == 0
			out = append(out, s)
			i++
		}
		return rows.Err()
	})
	return out, err
}

// TopCustomers returns the top spenders in the period.
func (r *ReportRepo) TopCustomers(ctx context.Context, scope tenant.TenantScope, from, to time.Time, limit int) ([]TopCustomer, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var out []TopCustomer
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		const q = `
			SELECT
				c.id, c.name, COALESCE(c.email::text,''),
				COUNT(o.id) AS orders,
				COALESCE(SUM(o.total_minor), 0) AS total_minor,
				MAX(o.created_at) AS last_order_at
			FROM customers c
			JOIN orders o ON o.customer_id = c.id
			WHERE c.organization_id = $1
			  AND o.created_at >= $2
			  AND o.created_at < $3
			  AND o.status <> 'CANCELLED'
			GROUP BY c.id, c.name, c.email
			ORDER BY total_minor DESC
			LIMIT $4
		`
		rows, err := tx.Query(ctx, q, scope.OrgID, from, to, limit)
		if err != nil {
			return fmt.Errorf("report_repo: top customers: %w", Classify(err))
		}
		defer rows.Close()
		for rows.Next() {
			var c TopCustomer
			if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Orders, &c.TotalMinor, &c.LastOrderAt); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Insights generates rule-based alerts for the business.
func (r *ReportRepo) Insights(ctx context.Context, scope tenant.TenantScope) ([]Insight, error) {
	var out []Insight
	err := r.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Rule 1: overdue orders.
		var overdue int64
		_ = tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			WHERE organization_id = $1
			  AND status NOT IN ('COMPLETED','CANCELLED','DELIVERED')
			  AND expected_completion IS NOT NULL
			  AND expected_completion < CURRENT_DATE
		`, scope.OrgID).Scan(&overdue)
		if overdue > 0 {
			out = append(out, Insight{
				Severity:   "warn",
				Title:      "Overdue orders",
				Message:    fmt.Sprintf("%d orders are past their expected completion date.", overdue),
				ActionURL:  "/orders",
				ActionText: "Review overdue",
			})
		}

		// Rule 2: orders ready with unpaid balance.
		var unpaidReady int64
		var unpaidReadyMinor int64
		_ = tx.QueryRow(ctx, `
			SELECT count(*), COALESCE(SUM(total_minor - amount_paid_minor), 0)
			FROM orders
			WHERE organization_id = $1
			  AND status IN ('READY','OUT_FOR_DELIVERY')
			  AND amount_paid_minor < total_minor
		`, scope.OrgID).Scan(&unpaidReady, &unpaidReadyMinor)
		if unpaidReady > 0 {
			out = append(out, Insight{
				Severity:   "critical",
				Title:      "Unpaid balances on ready orders",
				Message:    fmt.Sprintf("%d orders are ready but have outstanding balance.", unpaidReady),
				ActionURL:  "/orders",
				ActionText: "Collect balance",
			})
		}

		// Rule 3: high outstanding balance.
		var totalOutstanding int64
		_ = tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_minor - amount_paid_minor), 0)
			FROM orders
			WHERE organization_id = $1 AND status NOT IN ('CANCELLED')
		`, scope.OrgID).Scan(&totalOutstanding)
		if totalOutstanding > 0 {
			out = append(out, Insight{
				Severity:   "info",
				Title:      "Outstanding receivables",
				Message:    "There is money still owed to you across active orders.",
				ActionURL:  "/orders",
				ActionText: "See who owes",
			})
		}

		return nil
	})
	return out, err
}
