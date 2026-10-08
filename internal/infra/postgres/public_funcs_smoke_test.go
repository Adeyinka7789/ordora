package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/product"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
)

// openSmokeDB connects with the server's env vars (read-only usage).
// Skips when Postgres is unavailable, like the other DB-backed tests.
func openSmokeDB(t *testing.T) *postgres.DB {
	t.Helper()
	if os.Getenv("ORDORA_DB_USER") == "" {
		t.Skip("ORDORA_DB_USER not set; skipping DB-backed test")
	}
	dsn := "postgres://" + os.Getenv("ORDORA_DB_USER") + ":" +
		os.Getenv("ORDORA_DB_PASSWORD") + "@" +
		os.Getenv("ORDORA_DB_HOST") + ":" +
		os.Getenv("ORDORA_DB_PORT") + "/" +
		os.Getenv("ORDORA_DB_NAME") + "?sslmode=" +
		os.Getenv("ORDORA_DB_SSLMODE")
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("cannot connect to DB: %v", err)
	}
	return db
}

// TestPublicFuncs_Smoke executes the 0052 public function bodies with bogus
// arguments. CREATE FUNCTION only parses plpgsql — column/table references
// resolve at first execution, so these read-only calls (all expecting
// empty/not-found) are the regression net for SQL shape errors.
func TestPublicFuncs_Smoke(t *testing.T) {
	db := openSmokeDB(t)
	ctx := context.Background()

	publicRepo := postgres.NewPublicOrderRepo(db, id.Generator{})
	products, err := publicRepo.ListProductsBySlug(ctx, "definitely-not-a-real-slug-0052")
	if err != nil {
		t.Fatalf("get_public_products: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("bogus slug must yield no products, got %d", len(products))
	}

	portalRepo := postgres.NewPortalRepo(db)
	if _, err := portalRepo.GetPublicProductImage(ctx, uuid.New()); err == nil {
		t.Error("get_public_product_image with random id must 404")
	}
	if _, err := portalRepo.GetPortalAttachment(ctx, make([]byte, 32), uuid.New()); err == nil {
		t.Error("get_portal_attachment with zero hash must 404")
	}

	// The attachments entity CHECK must admit PRODUCT (read-only catalog
	// check — no rows written).
	var def string
	err = db.WithTenant(ctx, uuid.New(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid)
			  FROM pg_constraint
			 WHERE conrelid = 'attachments'::regclass
			   AND contype = 'c'
			   AND pg_get_constraintdef(oid) ILIKE '%entity_type%'
			 LIMIT 1
		`).Scan(&def)
	})
	if err != nil {
		t.Fatalf("constraint lookup: %v", err)
	}
	if !strings.Contains(def, "PRODUCT") {
		t.Errorf("attachments entity CHECK must include PRODUCT, got: %s", def)
	}
}

// TestProductAttachment_WritePath proves the full PRODUCT write path through
// the real repo: CHECK constraint + RLS + scan round-trip. Metadata only —
// no storage bytes are touched (repo.Delete is DB-only), and setupDB cleans
// up the isolated org afterwards.
func TestProductAttachment_WritePath(t *testing.T) {
	db, scope := setupDB(t)
	repo := postgres.NewAttachmentRepo(db)
	ctx := context.Background()

	a, err := attachment.New(
		uuid.New(), scope.OrgID,
		attachment.EntityProduct, uuid.New(),
		"test/"+uuid.NewString(), "cover.webp", "image/webp", 2048,
		scope.UserID, time.Now(),
	)
	if err != nil {
		t.Fatalf("domain: %v", err)
	}
	a.Purpose = attachment.PurposeProductCover
	if err := repo.Create(ctx, scope, a); err != nil {
		t.Fatalf("create PRODUCT attachment (CHECK/RLS): %v", err)
	}

	got, err := repo.GetByID(ctx, scope, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EntityType != attachment.EntityProduct || got.Purpose != attachment.PurposeProductCover {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	listed, err := repo.ListForEntity(ctx, scope, attachment.EntityProduct, a.EntityID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("list for product entity = %d, want 1", len(listed))
	}

	if err := repo.Delete(ctx, scope, a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// TestPublicOrder_QuotePricingAndCover proves the two 0054 SQL fixes
// through real rows in an isolated org:
//  1. quote_only products snapshot at unit_price 0 (request-quote lines
//     don't inflate the order total);
//  2. the lowercase product_cover purpose resolves cover_image_id on the
//     public listing and image_ref on the snapshotted line.
//
// audit_logs has no org FK, so the function's audit row is deleted
// explicitly (registered after setupDB → runs before its cleanup).
func TestPublicOrder_QuotePricingAndCover(t *testing.T) {
	db, scope := setupDB(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_ = db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM audit_logs WHERE organization_id = $1`, scope.OrgID)
			return err
		})
	})

	// Owner membership (create_public_order attributes to the first OWNER).
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO organization_members (organization_id, user_id, role, status)
			VALUES ($1, $2, 'OWNER', 'ACTIVE')
		`, scope.OrgID, scope.UserID)
		return err
	}); err != nil {
		t.Fatalf("membership: %v", err)
	}

	// Org slug for the public functions.
	var slug string
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT slug::text FROM organizations WHERE id = $1`, scope.OrgID).Scan(&slug)
	}); err != nil {
		t.Fatalf("slug: %v", err)
	}

	mkProduct := func(name string, priceMinor int64, quote bool) uuid.UUID {
		t.Helper()
		price, err := money.New(priceMinor, "NGN")
		if err != nil {
			t.Fatal(err)
		}
		p, err := product.New(uuid.New(), scope.OrgID, name, "desc", "", price, scope.UserID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := p.ApplyCatalog(product.CatalogDetails{QuoteOnly: quote}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := postgres.NewProductRepo(db).Create(ctx, scope, p); err != nil {
			t.Fatalf("product create: %v", err)
		}
		return p.ID
	}
	quoteID := mkProduct("Custom Gown", 50000, true)
	normalID := mkProduct("Senator", 30000, false)

	// Cover image on the quote product (lowercase purpose, as the app stores).
	coverID := uuid.New()
	cover, err := attachment.New(coverID, scope.OrgID,
		attachment.EntityProduct, quoteID,
		"test/"+uuid.NewString(), "gown.webp", "image/webp", 1024,
		scope.UserID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cover.Purpose = attachment.PurposeProductCover
	if err := postgres.NewAttachmentRepo(db).Create(ctx, scope, cover); err != nil {
		t.Fatalf("cover create: %v", err)
	}

	// 1. Public listing resolves the cover (0054 casing fix).
	listed, err := postgres.NewPublicOrderRepo(db, id.Generator{}).ListProductsBySlug(ctx, slug)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	byID := map[uuid.UUID]bool{}
	for _, p := range listed {
		byID[p.ID] = true
		if p.ID == quoteID {
			if !p.QuoteOnly {
				t.Error("quote product must be flagged quote_only")
			}
			if p.CoverImageID != coverID {
				t.Errorf("cover_image_id = %v, want %v (purpose casing)", p.CoverImageID, coverID)
			}
		}
	}
	if !byID[quoteID] || !byID[normalID] {
		t.Fatalf("both products must be publicly listed (got %d)", len(listed))
	}

	// 2. Public order: quote line snapshots at 0, normal line at price.
	items, _ := json.Marshal([]map[string]string{
		{"id": uuid.NewString(), "product_id": quoteID.String(), "qty": "1.000"},
		{"id": uuid.NewString(), "product_id": normalID.String(), "qty": "2.000"},
	})
	answers, _ := json.Marshal([]any{})
	var orderID uuid.UUID
	var total int64
	err = db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT order_id, total_minor FROM create_public_order($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12::jsonb)`,
			slug, "Ada", "ada@example.local", "", "", nil, nil, string(items),
			uuid.New(), uuid.New(), uuid.New(), string(answers),
		).Scan(&orderID, &total)
	})
	if err != nil {
		t.Fatalf("create_public_order: %v", err)
	}
	if total != 60000 {
		t.Errorf("total = %d, want 60000 (2×30000 + quote at 0)", total)
	}

	type line struct {
		desc     string
		unit     int64
		sub      int64
		material string
		image    string
	}
	var lines []line
	if err := db.WithTenant(ctx, scope.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT description, unit_price_minor, subtotal_minor,
			       COALESCE(material,''), COALESCE(image_ref,'')
			  FROM order_items WHERE order_id = $1 ORDER BY position
		`, orderID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l line
			if err := rows.Scan(&l.desc, &l.unit, &l.sub, &l.material, &l.image); err != nil {
				return err
			}
			lines = append(lines, l)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("lines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].unit != 0 || lines[0].sub != 0 {
		t.Errorf("quote line must snapshot at 0, got %+v", lines[0])
	}
	if lines[0].image != coverID.String() {
		t.Errorf("quote line image_ref = %q, want cover %v", lines[0].image, coverID)
	}
	if lines[1].unit != 30000 || lines[1].sub != 60000 {
		t.Errorf("normal line wrong: %+v", lines[1])
	}
}
