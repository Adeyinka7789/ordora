package postgres

import (
	"context"

	"github.com/google/uuid"
)

// AdminAuditAdapter wraps AdminAuditRepo to satisfy app.AdminAuditWriter.
type AdminAuditAdapter struct {
	repo *AdminAuditRepo
}

func NewAdminAuditAdapter(repo *AdminAuditRepo) *AdminAuditAdapter {
	return &AdminAuditAdapter{repo: repo}
}

func (a *AdminAuditAdapter) Record(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID, metadata map[string]any, ip string) error {
	adminIDCopy := adminID
	targetIDCopy := targetID
	var targetTypePtr *string
	if targetType != "" {
		t := targetType
		targetTypePtr = &t
	}
	return a.repo.Record(ctx, AdminAuditEntry{
		AdminID:    &adminIDCopy,
		Action:     action,
		TargetType: derefOr(targetTypePtr, ""),
		TargetID:   &targetIDCopy,
		Metadata:   metadata,
		IP:         ip,
	})
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
