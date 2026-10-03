package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// AdminUserAdapter wraps AdminUserRepo to satisfy app.AdminUserReader.
type AdminUserAdapter struct {
	repo *AdminUserRepo
}

func NewAdminUserAdapter(repo *AdminUserRepo) *AdminUserAdapter {
	return &AdminUserAdapter{repo: repo}
}

func (a *AdminUserAdapter) ListUsers(ctx context.Context, query string, limit, offset int) ([]app.AdminUserRow, int, error) {
	rows, total, err := a.repo.ListUsers(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]app.AdminUserRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.AdminUserRow{
			ID:              r.ID,
			Email:           r.Email,
			Name:            r.Name,
			EmailVerified:   r.EmailVerified,
			MembershipCount: r.MembershipCount,
			SessionCount:    r.SessionCount,
			CreatedAt:       r.CreatedAt,
		})
	}
	return out, total, nil
}

func (a *AdminUserAdapter) GetUser(ctx context.Context, id uuid.UUID) (*app.AdminUserDetail, error) {
	u, err := a.repo.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &app.AdminUserDetail{
		ID:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		EmailVerified: u.EmailVerified,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
	for _, m := range u.Memberships {
		out.Memberships = append(out.Memberships, app.AdminMembershipRow{
			OrganizationID:   m.OrganizationID,
			OrganizationName: m.OrganizationName,
			OrganizationSlug: m.OrganizationSlug,
			Role:             m.Role,
			Status:           m.Status,
			CreatedAt:        m.CreatedAt,
		})
	}
	for _, s := range u.Sessions {
		out.Sessions = append(out.Sessions, app.AdminSessionRow{
			ID:        s.ID,
			UserAgent: s.UserAgent,
			IP:        s.IP,
			ExpiresAt: s.ExpiresAt,
			CreatedAt: s.CreatedAt,
		})
	}
	return out, nil
}

func (a *AdminUserAdapter) RevokeAllSessions(ctx context.Context, userID uuid.UUID, now time.Time) (int64, error) {
	return a.repo.RevokeAllSessions(ctx, userID, now)
}
