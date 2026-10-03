package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// AdminOpsRepo reads operational state for the admin ops dashboard.
type AdminOpsRepo struct {
	adminDB *DB
	appDB   *DB // the app pool, used to read PoolStats for the real pool
}

func NewAdminOpsRepo(adminDB, appDB *DB) *AdminOpsRepo {
	return &AdminOpsRepo{adminDB: adminDB, appDB: appDB}
}

// OutboxSummary is a snapshot of the outbox.
type OutboxSummary struct {
	Pending       int64
	Dispatched    int64
	Failed        int64
	OldestPending time.Time
}

// NotificationSummary is a snapshot of the job queue.
type NotificationSummary struct {
	Pending int64
	Sent    int64
	Failed  int64
	Dead    int64
}

// HealthSummary is a snapshot of DB + timing.
type HealthSummary struct {
	DBOK            bool
	DBPingMs        int64
	Organizations   int64
	Users           int64
	Customers       int64
	Orders          int64
	Payments        int64
	TotalGMVMinor   int64
	TotalPaidMinor  int64
	AppPoolAcquired int32
	AppPoolIdle     int32
	AppPoolTotal    int32
	AppPoolMaxConns int32
}

// Outbox returns outbox counts.
func (r *AdminOpsRepo) Outbox(ctx context.Context) (*OutboxSummary, error) {
	var s OutboxSummary
	var oldest *time.Time
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT
				COUNT(*) FILTER (WHERE dispatched_at IS NULL) AS pending,
				COUNT(*) FILTER (WHERE dispatched_at IS NOT NULL) AS dispatched,
				COUNT(*) FILTER (WHERE dispatched_at IS NULL AND attempts >= 5) AS failed,
				MIN(occurred_at) FILTER (WHERE dispatched_at IS NULL) AS oldest_pending
			FROM outbox
		`
		return tx.QueryRow(ctx, q).Scan(&s.Pending, &s.Dispatched, &s.Failed, &oldest)
	})
	s.OldestPending = timeOrZero(oldest)
	return &s, err
}

// Notifications returns notification job counts.
func (r *AdminOpsRepo) Notifications(ctx context.Context) (*NotificationSummary, error) {
	var s NotificationSummary
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT
				COUNT(*) FILTER (WHERE status = 'PENDING') AS pending,
				COUNT(*) FILTER (WHERE status = 'SENT')    AS sent,
				COUNT(*) FILTER (WHERE status = 'FAILED')  AS failed,
				COUNT(*) FILTER (WHERE status = 'DEAD')    AS dead
			FROM notification_jobs
		`
		return tx.QueryRow(ctx, q).Scan(&s.Pending, &s.Sent, &s.Failed, &s.Dead)
	})
	return &s, err
}

// Health returns aggregate metrics.
func (r *AdminOpsRepo) Health(ctx context.Context) (*HealthSummary, error) {
	var s HealthSummary

	// DB ping timing.
	start := time.Now()
	if err := r.appDB.Ping(ctx); err != nil {
		s.DBOK = false
	} else {
		s.DBOK = true
		s.DBPingMs = time.Since(start).Milliseconds()
	}

	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT
				(SELECT count(*) FROM organizations) AS orgs,
				(SELECT count(*) FROM users) AS users,
				(SELECT count(*) FROM customers) AS customers,
				(SELECT count(*) FROM orders) AS orders,
				(SELECT count(*) FROM payments) AS payments,
				COALESCE((SELECT sum(total_minor) FROM orders WHERE status <> 'CANCELLED'), 0) AS gmv,
				COALESCE((SELECT sum(amount_minor) FROM payments WHERE reversed_by IS NULL AND reverses IS NULL), 0) AS paid
		`
		return tx.QueryRow(ctx, q).Scan(
			&s.Organizations, &s.Users, &s.Customers, &s.Orders, &s.Payments,
			&s.TotalGMVMinor, &s.TotalPaidMinor,
		)
	})
	if err != nil {
		return nil, err
	}

	// App pool stats.
	st := r.appDB.Pool().Stat()
	s.AppPoolAcquired = st.AcquiredConns()
	s.AppPoolIdle = st.IdleConns()
	s.AppPoolTotal = st.TotalConns()
	s.AppPoolMaxConns = st.MaxConns()

	return &s, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
