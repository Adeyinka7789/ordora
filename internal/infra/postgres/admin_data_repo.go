package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// AdminDataRepo provides read-only access to whitelisted tables.
//
// Security: the table name and column names are read from the
// admin_browsable_tables table, never from user input. The only user
// input that reaches the query is the search string, which goes through
// a parameterized argument.
//
// Sensitive data is never exposed: session/token tables are blocked
// entirely, and secret columns (hashes, TOTP, reset tokens) are stripped
// from every browse, even if the whitelist row still exists.
type AdminDataRepo struct {
	adminDB *DB
}

func NewAdminDataRepo(adminDB *DB) *AdminDataRepo {
	return &AdminDataRepo{adminDB: adminDB}
}

// BrowsableTable is one row of admin_browsable_tables.
type BrowsableTable struct {
	TableName     string
	DisplayName   string
	SearchColumns []string
	OrderBy       string
	OrderDir      string
	MaxLimit      int
}

// ListTables returns the whitelist, minus denied session/token tables.
// Filtering here keeps the UI from showing dead links on databases that
// predate the 0051 cleanup migration (GetTable/Browse fail closed anyway).
func (r *AdminDataRepo) ListTables(ctx context.Context) ([]BrowsableTable, error) {
	var out []BrowsableTable
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT table_name, display_name, search_columns, order_by, order_dir, max_limit
			FROM admin_browsable_tables
			ORDER BY display_name
		`
		rows, err := tx.Query(ctx, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t BrowsableTable
			if err := rows.Scan(&t.TableName, &t.DisplayName, &t.SearchColumns, &t.OrderBy, &t.OrderDir, &t.MaxLimit); err != nil {
				return err
			}
			if _, denied := deniedTables[strings.ToLower(t.TableName)]; denied {
				continue
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// deniedTables can never be browsed, even if a whitelist row exists.
// They hold session tokens, password-reset secrets, and IP metadata.
var deniedTables = map[string]struct{}{
	"sessions":               {},
	"admin_sessions":         {},
	"impersonation_sessions": {},
	"auth_tokens":            {},
}

// isDeniedColumn reports whether a column must never leave the server.
// Covers password hashes, session/public/join/manage token hashes, TOTP
// secrets, and any future *_secret / *token_hash column.
func isDeniedColumn(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "password_hash", "totp_secret":
		return true
	}
	if strings.Contains(lower, "token_hash") {
		return true
	}
	if strings.Contains(lower, "secret") {
		return true
	}
	return false
}

// GetTable returns one whitelist entry.
func (r *AdminDataRepo) GetTable(ctx context.Context, name string) (*BrowsableTable, error) {
	if _, denied := deniedTables[strings.ToLower(name)]; denied {
		return nil, ErrNotFound
	}
	var t *BrowsableTable
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT table_name, display_name, search_columns, order_by, order_dir, max_limit
			FROM admin_browsable_tables
			WHERE table_name = $1
		`
		var row BrowsableTable
		if err := tx.QueryRow(ctx, q, name).Scan(&row.TableName, &row.DisplayName,
			&row.SearchColumns, &row.OrderBy, &row.OrderDir, &row.MaxLimit); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		t = &row
		return nil
	})
	return t, err
}

// ColumnInfo describes a table's columns.
type ColumnInfo struct {
	Name     string
	DataType string
	Nullable bool
	IsPK     bool
}

// DescribeTable reads columns from information_schema for the given table.
// Table name is validated against the whitelist before this runs.
//
// Secret columns are stripped here — this is the single choke point, so
// neither the UI headers (via AdminDataService.Browse) nor the SELECT list
// (via BrowseTable below) can ever expose names like password_hash or
// token_hash. Denied tables fail closed with ErrNotFound.
func (r *AdminDataRepo) DescribeTable(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	if _, denied := deniedTables[strings.ToLower(tableName)]; denied {
		return nil, ErrNotFound
	}
	var out []ColumnInfo
	err := r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT
				c.column_name,
				c.data_type,
				c.is_nullable = 'YES',
				EXISTS (
					SELECT 1
					FROM information_schema.table_constraints tc
					JOIN information_schema.key_column_usage kcu
					  ON kcu.constraint_name = tc.constraint_name
					WHERE tc.table_name = c.table_name
					  AND tc.constraint_type = 'PRIMARY KEY'
					  AND kcu.column_name = c.column_name
				) AS is_pk
			FROM information_schema.columns c
			WHERE c.table_schema = 'public' AND c.table_name = $1
			ORDER BY c.ordinal_position
		`
		rows, err := tx.Query(ctx, q, tableName)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var col ColumnInfo
			if err := rows.Scan(&col.Name, &col.DataType, &col.Nullable, &col.IsPK); err != nil {
				return err
			}
			if isDeniedColumn(col.Name) {
				continue
			}
			out = append(out, col)
		}
		return rows.Err()
	})
	return out, err
}

// RowResult is the result of a table browse.
type RowResult struct {
	Columns []string
	Rows    [][]any
	Total   int
	Limit   int
	Offset  int
}

// BrowseTable reads a page of rows from a whitelisted table.
//
// All values flow through parameters. The table name and columns are
// validated against the whitelist + information_schema before use.
func (r *AdminDataRepo) BrowseTable(ctx context.Context, t *BrowsableTable, search string, limit, offset int) (*RowResult, error) {
	if _, denied := deniedTables[strings.ToLower(t.TableName)]; denied {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > t.MaxLimit {
		limit = t.MaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	// Fetch columns.
	// DescribeTable already strips secret columns and rejects denied tables,
	// so cols here are exactly the visible set. A table with no visible
	// columns is treated as non-browsable.
	cols, err := r.DescribeTable(ctx, t.TableName)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, ErrNotFound
	}
	colNames := make([]string, 0, len(cols))
	for _, c := range cols {
		colNames = append(colNames, c.Name)
	}

	// Validate order_by against the columns.
	orderBy := t.OrderBy
	if !contains(colNames, orderBy) {
		// Fall back to the first PK column, or the first column.
		orderBy = ""
		for _, c := range cols {
			if c.IsPK {
				orderBy = c.Name
				break
			}
		}
		if orderBy == "" {
			orderBy = colNames[0]
		}
	}
	orderDir := t.OrderDir
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}

	// Build the WHERE clause. Only whitelisted text columns are searchable.
	var (
		whereSQL string
		args     []any
	)
	if search != "" && len(t.SearchColumns) > 0 {
		parts := make([]string, 0, len(t.SearchColumns))
		for i, c := range t.SearchColumns {
			if !contains(colNames, c) {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s::text ILIKE $%d", quoteIdent(c), i+1))
		}
		if len(parts) > 0 {
			whereSQL = " WHERE " + strings.Join(parts, " OR ")
			for range parts {
				args = append(args, "%"+search+"%")
			}
		}
	}

	// Quote the table name. We know it's whitelisted, but still quote.
	qt := quoteIdent(t.TableName)

	var out *RowResult
	err = r.adminDB.WithTx(ctx, func(tx pgx.Tx) error {
		// Count.
		countSQL := "SELECT count(*) FROM " + qt + whereSQL
		var total int
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
			return err
		}

		// Page.
		listSQL := "SELECT " + joinQuoted(colNames) + " FROM " + qt + whereSQL +
			" ORDER BY " + quoteIdent(orderBy) + " " + orderDir +
			fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
		pageArgs := append([]any{}, args...)
		pageArgs = append(pageArgs, limit, offset)

		rows, err := tx.Query(ctx, listSQL, pageArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()

		var data [][]any
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			data = append(data, values)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		out = &RowResult{
			Columns: colNames,
			Rows:    data,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		}
		return nil
	})
	return out, err
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func quoteIdent(s string) string {
	// Escape internal double quotes and wrap in double quotes.
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func joinQuoted(cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = quoteIdent(c)
	}
	return strings.Join(parts, ", ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
