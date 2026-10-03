package postgres

import (
	"context"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// AdminDataAdapter wraps AdminDataRepo to satisfy app.AdminDataReader.
type AdminDataAdapter struct {
	repo *AdminDataRepo
}

func NewAdminDataAdapter(repo *AdminDataRepo) *AdminDataAdapter {
	return &AdminDataAdapter{repo: repo}
}

func (a *AdminDataAdapter) ListTables(ctx context.Context) ([]app.BrowsableTable, error) {
	rows, err := a.repo.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]app.BrowsableTable, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.BrowsableTable{
			TableName:     r.TableName,
			DisplayName:   r.DisplayName,
			SearchColumns: r.SearchColumns,
			OrderBy:       r.OrderBy,
			OrderDir:      r.OrderDir,
			MaxLimit:      r.MaxLimit,
		})
	}
	return out, nil
}

func (a *AdminDataAdapter) GetTable(ctx context.Context, name string) (*app.BrowsableTable, error) {
	r, err := a.repo.GetTable(ctx, name)
	if err != nil {
		return nil, err
	}
	return &app.BrowsableTable{
		TableName:     r.TableName,
		DisplayName:   r.DisplayName,
		SearchColumns: r.SearchColumns,
		OrderBy:       r.OrderBy,
		OrderDir:      r.OrderDir,
		MaxLimit:      r.MaxLimit,
	}, nil
}

func (a *AdminDataAdapter) DescribeTable(ctx context.Context, tableName string) ([]app.TableColumn, error) {
	rows, err := a.repo.DescribeTable(ctx, tableName)
	if err != nil {
		return nil, err
	}
	out := make([]app.TableColumn, 0, len(rows))
	for _, r := range rows {
		out = append(out, app.TableColumn{
			Name:     r.Name,
			DataType: r.DataType,
			Nullable: r.Nullable,
			IsPK:     r.IsPK,
		})
	}
	return out, nil
}

func (a *AdminDataAdapter) BrowseTable(ctx context.Context, t *app.BrowsableTable, search string, limit, offset int) (*app.TableRows, error) {
	pt := &BrowsableTable{
		TableName:     t.TableName,
		DisplayName:   t.DisplayName,
		SearchColumns: t.SearchColumns,
		OrderBy:       t.OrderBy,
		OrderDir:      t.OrderDir,
		MaxLimit:      t.MaxLimit,
	}
	res, err := a.repo.BrowseTable(ctx, pt, search, limit, offset)
	if err != nil {
		return nil, err
	}
	return &app.TableRows{
		Columns: res.Columns,
		Rows:    res.Rows,
		Total:   res.Total,
		Limit:   res.Limit,
		Offset:  res.Offset,
	}, nil
}
