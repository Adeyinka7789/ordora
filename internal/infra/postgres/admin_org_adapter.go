package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// AdminOrgAdapter wraps AdminOrgRepo so it satisfies app.AdminOrgReader.
type AdminOrgAdapter struct {
	repo *AdminOrgRepo
}

func NewAdminOrgAdapter(repo *AdminOrgRepo) *AdminOrgAdapter {
	return &AdminOrgAdapter{repo: repo}
}

func (a *AdminOrgAdapter) ListOrgs(ctx context.Context, query string, limit, offset int) ([]app.AdminOrgRow, int, error) {
	rows, total, err := a.repo.ListOrgs(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]app.AdminOrgRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.AdminOrgRow{
			ID:             r.ID,
			Name:           r.Name,
			Slug:           r.Slug,
			Currency:       r.Currency,
			Email:          r.Email,
			Phone:          r.Phone,
			MemberCount:    r.MemberCount,
			CustomerCount:  r.CustomerCount,
			OrderCount:     r.OrderCount,
			TotalOrdersGMV: r.TotalOrdersGMV,
			OutstandingGMV: r.OutstandingGMV,
			CreatedAt:      r.CreatedAt,
		})
	}
	return out, total, nil
}

func (a *AdminOrgAdapter) GetOrg(ctx context.Context, id uuid.UUID) (*app.AdminOrgDetail, error) {
	o, row, err := a.repo.GetOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	return &app.AdminOrgDetail{
		ID:             o.ID,
		Name:           o.Name,
		Slug:           o.Slug.String(),
		Currency:       o.Currency,
		Timezone:       o.Timezone,
		Email:          o.Email,
		Phone:          o.Phone,
		Address:        o.Address,
		MemberCount:    row.MemberCount,
		CustomerCount:  row.CustomerCount,
		OrderCount:     row.OrderCount,
		TotalOrdersGMV: row.TotalOrdersGMV,
		OutstandingGMV: row.OutstandingGMV,
		CreatedAt:      o.CreatedAt,
	}, nil
}

func (a *AdminOrgAdapter) ListMembersForOrg(ctx context.Context, orgID uuid.UUID) ([]app.AdminMemberRow, error) {
	rows, err := a.repo.ListMembersForOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]app.AdminMemberRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.AdminMemberRow{
			UserID:        r.UserID,
			Role:          r.Role,
			Status:        r.Status,
			Email:         r.Email,
			Name:          r.Name,
			EmailVerified: r.EmailVerified,
			CreatedAt:     r.CreatedAt,
		})
	}
	return out, nil
}

func (a *AdminOrgAdapter) SuspendOrg(ctx context.Context, orgID uuid.UUID, reason string) error {
	return a.repo.SuspendOrg(ctx, orgID, reason)
}

func (a *AdminOrgAdapter) UnsuspendOrg(ctx context.Context, orgID uuid.UUID) error {
	return a.repo.UnsuspendOrg(ctx, orgID)
}

func (a *AdminOrgAdapter) DeleteOrg(ctx context.Context, orgID uuid.UUID) error {
	return a.repo.DeleteOrg(ctx, orgID)
}
