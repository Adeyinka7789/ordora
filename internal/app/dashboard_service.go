package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// DashboardStats is the shape of the counter row on the dashboard.
type DashboardStats struct {
	TotalOrders      int64
	PendingOrders    int64 // not COMPLETED or CANCELLED
	DueToday         int64
	Overdue          int64 // expected_completion < today, not terminal
	OutstandingMinor int64
	Currency         string
	RecentOrders     []DashboardRecentOrder
}

// DashboardRecentOrder is a lightweight row for the recent-orders table.
type DashboardRecentOrder struct {
	ID           uuid.UUID
	Number       string
	CustomerName string
	Title        string
	Status       order.Status
	TotalMinor   int64
	PaidMinor    int64
	ExpectedDate *time.Time
	CreatedAt    time.Time
}

// DashboardService computes dashboard aggregates for one tenant.
type DashboardService struct {
	db DashboardDB
}

// DashboardDB is the minimal DB surface the service needs.
type DashboardDB interface {
	WithTenant(ctx context.Context, orgID uuid.UUID, fn func(pgx.Tx) error) error
}

func NewDashboardService(db DashboardDB) *DashboardService {
	return &DashboardService{db: db}
}

// Load computes all dashboard counters in one tenant transaction.
func (s *DashboardService) Load(ctx context.Context, scope tenant.TenantScope, currency string) (*DashboardStats, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	stats := &DashboardStats{Currency: currency}

	err := s.db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		// Total orders.
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&stats.TotalOrders); err != nil {
			return err
		}

		// Pending = not in terminal states.
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			WHERE status NOT IN ('COMPLETED', 'CANCELLED')
		`).Scan(&stats.PendingOrders); err != nil {
			return err
		}

		// Due today: expected_completion = today, not terminal.
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			WHERE expected_completion = $1
			  AND status NOT IN ('COMPLETED', 'CANCELLED')
		`, today).Scan(&stats.DueToday); err != nil {
			return err
		}

		// Overdue: expected_completion < today, not terminal, and not delivered.
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM orders
			WHERE expected_completion < $1
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'DELIVERED')
		`, today).Scan(&stats.Overdue); err != nil {
			return err
		}

		// Outstanding balance: SUM(total - paid) over non-cancelled orders.
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_minor - amount_paid_minor), 0)
			FROM orders
			WHERE status NOT IN ('CANCELLED')
		`).Scan(&stats.OutstandingMinor); err != nil {
			return err
		}

		// Recent orders (10).
		rows, err := tx.Query(ctx, `
			SELECT o.id, o.order_number, c.name, o.title, o.status,
			       o.total_minor, o.amount_paid_minor, o.expected_completion, o.created_at
			FROM orders o
			JOIN customers c ON c.id = o.customer_id
			ORDER BY o.created_at DESC
			LIMIT 10
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var r DashboardRecentOrder
			var statusStr string
			if err := rows.Scan(
				&r.ID, &r.Number, &r.CustomerName, &r.Title, &statusStr,
				&r.TotalMinor, &r.PaidMinor, &r.ExpectedDate, &r.CreatedAt,
			); err != nil {
				return err
			}
			r.Status = order.Status(statusStr)
			stats.RecentOrders = append(stats.RecentOrders, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// Money convenience — returns the outstanding amount as a money.Money.
func (s *DashboardStats) Outstanding() money.Money {
	m, _ := money.New(s.OutstandingMinor, s.Currency)
	return m
}
