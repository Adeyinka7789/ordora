package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

func TestZZPrintConstraints(t *testing.T) {
	db, _ := setupDB(t)
	ctx := context.Background()
	var dsn string
	_ = dsn
	_ = db
	_ = ctx
	_ = pgx.Tx(nil)
	fmt.Println("placeholder")
}
