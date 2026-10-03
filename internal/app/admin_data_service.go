package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// BrowsableTable is a whitelist entry.
type BrowsableTable struct {
	TableName     string
	DisplayName   string
	SearchColumns []string
	OrderBy       string
	OrderDir      string
	MaxLimit      int
}

// TableColumn describes a column.
type TableColumn struct {
	Name     string
	DataType string
	Nullable bool
	IsPK     bool
}

// TableRows is a page of data.
type TableRows struct {
	Columns []string
	Rows    [][]any
	Total   int
	Limit   int
	Offset  int
}

// AdminDataReader is the persistence contract for the data browser.
type AdminDataReader interface {
	ListTables(ctx context.Context) ([]BrowsableTable, error)
	GetTable(ctx context.Context, name string) (*BrowsableTable, error)
	DescribeTable(ctx context.Context, tableName string) ([]TableColumn, error)
	BrowseTable(ctx context.Context, t *BrowsableTable, search string, limit, offset int) (*TableRows, error)
}

// AdminDataService orchestrates the data browser.
type AdminDataService struct {
	repo  AdminDataReader
	audit AdminAuditWriter
}

type AdminDataServiceDeps struct {
	Repo  AdminDataReader
	Audit AdminAuditWriter
}

func NewAdminDataService(d AdminDataServiceDeps) *AdminDataService {
	return &AdminDataService{repo: d.Repo, audit: d.Audit}
}

var ErrTableNotWhitelisted = errors.New("admin: table not whitelisted")

func (s *AdminDataService) ListTables(ctx context.Context) ([]BrowsableTable, error) {
	return s.repo.ListTables(ctx)
}

func (s *AdminDataService) GetTable(ctx context.Context, name string) (*BrowsableTable, error) {
	return s.repo.GetTable(ctx, name)
}

func (s *AdminDataService) Browse(ctx context.Context, adminID uuid.UUID, tableName, search string, limit, offset int) (*BrowsableTable, []TableColumn, *TableRows, error) {
	t, err := s.repo.GetTable(ctx, tableName)
	if err != nil {
		return nil, nil, nil, ErrTableNotWhitelisted
	}
	cols, err := s.repo.DescribeTable(ctx, tableName)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := s.repo.BrowseTable(ctx, t, search, limit, offset)
	if err != nil {
		return nil, nil, nil, err
	}
	// Audit: log the browse. Best-effort.
	if s.audit != nil {
		_ = s.audit.Record(ctx, adminID, "data.browse", "TABLE", uuid.Nil,
			map[string]any{"table": tableName, "search": search, "rows": len(rows.Rows)}, "")
	}
	return t, cols, rows, nil
}
