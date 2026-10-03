package postgres

import (
	"context"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// AdminOpsAdapter wraps AdminOpsRepo and AdminAuditRepo to satisfy
// app.AdminOpsReader.
type AdminOpsAdapter struct {
	ops   *AdminOpsRepo
	audit *AdminAuditRepo
}

func NewAdminOpsAdapter(ops *AdminOpsRepo, audit *AdminAuditRepo) *AdminOpsAdapter {
	return &AdminOpsAdapter{ops: ops, audit: audit}
}

func (a *AdminOpsAdapter) Outbox(ctx context.Context) (*app.OutboxSummary, error) {
	s, err := a.ops.Outbox(ctx)
	if err != nil {
		return nil, err
	}
	return &app.OutboxSummary{
		Pending:       s.Pending,
		Dispatched:    s.Dispatched,
		Failed:        s.Failed,
		OldestPending: s.OldestPending,
	}, nil
}

func (a *AdminOpsAdapter) Notifications(ctx context.Context) (*app.NotificationSummary, error) {
	s, err := a.ops.Notifications(ctx)
	if err != nil {
		return nil, err
	}
	return &app.NotificationSummary{
		Pending: s.Pending,
		Sent:    s.Sent,
		Failed:  s.Failed,
		Dead:    s.Dead,
	}, nil
}

func (a *AdminOpsAdapter) Health(ctx context.Context) (*app.HealthSummary, error) {
	s, err := a.ops.Health(ctx)
	if err != nil {
		return nil, err
	}
	return &app.HealthSummary{
		DBOK:            s.DBOK,
		DBPingMs:        s.DBPingMs,
		Organizations:   s.Organizations,
		Users:           s.Users,
		Customers:       s.Customers,
		Orders:          s.Orders,
		Payments:        s.Payments,
		TotalGMVMinor:   s.TotalGMVMinor,
		TotalPaidMinor:  s.TotalPaidMinor,
		AppPoolAcquired: s.AppPoolAcquired,
		AppPoolIdle:     s.AppPoolIdle,
		AppPoolTotal:    s.AppPoolTotal,
		AppPoolMaxConns: s.AppPoolMaxConns,
	}, nil
}

func (a *AdminOpsAdapter) ListAudit(ctx context.Context, action, targetType string, limit, offset int) ([]app.AdminAuditEntry, int, error) {
	rows, total, err := a.audit.ListFiltered(ctx, AdminAuditFilter{
		Action:     action,
		TargetType: targetType,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]app.AdminAuditEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.AdminAuditEntry{
			ID:         r.ID,
			AdminID:    r.AdminID,
			Action:     r.Action,
			TargetType: r.TargetType,
			TargetID:   r.TargetID,
			Metadata:   r.Metadata,
			IP:         r.IP,
			CreatedAt:  r.CreatedAt,
		})
	}
	return out, total, nil
}
