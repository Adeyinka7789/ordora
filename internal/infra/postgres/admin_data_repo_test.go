package postgres

import (
	"strings"
	"testing"
)

// TestDeniedTables_CannotBeBrowsed pins the admin data-browser blocklist:
// session/token tables must never be readable, even if a whitelist row
// still exists (e.g. databases predating the 0051 cleanup migration).
func TestDeniedTables_CannotBeBrowsed(t *testing.T) {
	for _, table := range []string{
		"sessions", "admin_sessions", "impersonation_sessions", "auth_tokens",
	} {
		if _, ok := deniedTables[table]; !ok {
			t.Errorf("deniedTables must contain %q", table)
		}
		if _, ok := deniedTables[strings.ToUpper(table)]; ok {
			t.Errorf("deniedTables lookup must be lowercase-normalized by callers, %q must not match directly", strings.ToUpper(table))
		}
	}
}

// TestDeniedColumns_NeverLeaveServer pins the secret-column filter used by
// DescribeTable (UI headers) and BrowseTable (SELECT list). Values in these
// columns are password hashes, token hashes, and 2FA secrets.
func TestDeniedColumns_NeverLeaveServer(t *testing.T) {
	denied := []string{
		"password_hash",
		"token_hash",
		"public_token_hash",
		"join_token_hash",
		"manage_token_hash",
		"totp_secret",
		"future_secret_key",
	}
	for _, col := range denied {
		if !isDeniedColumn(col) {
			t.Errorf("isDeniedColumn(%q) must be true", col)
		}
	}

	// Ordinary business columns must stay visible — the filter must not
	// over-match and blank out tables.
	visible := []string{
		"id", "email", "name", "created_at", "order_number",
		"user_agent", "ip", "filename", "mime_type",
	}
	for _, col := range visible {
		if isDeniedColumn(col) {
			t.Errorf("isDeniedColumn(%q) must be false", col)
		}
	}
}
