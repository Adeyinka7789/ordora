package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Adeyinka7789/ordora/internal/app"
)

// PublicOrderRepo implements app.PublicOrderDB. All its operations go through
// SECURITY DEFINER functions so they can run without a tenant scope.
type PublicOrderRepo struct {
	db  *DB
	ids app.IDGen
}

func NewPublicOrderRepo(db *DB, ids app.IDGen) *PublicOrderRepo {
	return &PublicOrderRepo{db: db, ids: ids}
}

// LookupOrgBySlug resolves a business slug to (id, name, currency).
// Columns are named explicitly so a stale function shape fails loudly in
// logs instead of silently shifting values.
func (r *PublicOrderRepo) LookupOrgBySlug(ctx context.Context, slug string) (*app.PublicOrg, error) {
	var out *app.PublicOrg
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `SELECT org_id, org_name, currency, email, slug FROM lookup_public_org($1)`
		var o app.PublicOrg
		if err := tx.QueryRow(ctx, q, slug).Scan(&o.ID, &o.Name, &o.Currency, &o.Email, &o.Slug); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrPublicOrgNotFound
			}
			return fmt.Errorf("public_order_repo: lookup: %w", err)
		}
		out = &o
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, app.ErrPublicOrgNotFound
	}
	return out, nil
}

// ListProductsBySlug returns the shop's visible catalog for the public
// intake form. Unknown slugs yield an empty list.
func (r *PublicOrderRepo) ListProductsBySlug(ctx context.Context, slug string) ([]app.PublicProduct, error) {
	var out []app.PublicProduct
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT product_id, product_name, product_description, product_short,
			       product_material, product_category, unit_price_minor, currency,
			       quote_only, starting_from, availability, cover_image_id, product_questions,
			       product_specs, product_color, product_production_days, product_image_ids
			FROM get_public_products($1)
		`
		rows, err := tx.Query(ctx, q, slug)
		if err != nil {
			return fmt.Errorf("public_order_repo: products: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p app.PublicProduct
			var coverID *uuid.UUID
			var questionsRaw []byte
			var imageIDs string
			if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.ShortDescription,
				&p.Material, &p.Category, &p.UnitPriceMinor, &p.Currency,
				&p.QuoteOnly, &p.StartingFrom, &p.Availability,
				&coverID, &questionsRaw, &p.Specs, &p.Color, &p.ProductionDays,
				&imageIDs); err != nil {
				return fmt.Errorf("public_order_repo: scan product: %w", err)
			}
			if coverID != nil {
				p.CoverImageID = *coverID
			}
			p.Questions = unmarshalPublicQuestions(questionsRaw)
			p.ImageIDs = parseImageIDs(imageIDs)
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// parseImageIDs splits the comma-joined gallery ids from
// get_public_products, dropping blanks and malformed entries.
func parseImageIDs(raw string) []uuid.UUID {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []uuid.UUID
	for _, part := range strings.Split(raw, ",") {
		if id, err := uuid.Parse(strings.TrimSpace(part)); err == nil && id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}

// unmarshalPublicQuestions decodes the questions JSONB from
// get_public_products. The keys match product.ProductQuestion encoding.
func unmarshalPublicQuestions(raw []byte) []app.ProductQuestion {
	if len(raw) == 0 {
		return nil
	}
	var qs []app.ProductQuestion
	if err := json.Unmarshal(raw, &qs); err != nil {
		return nil
	}
	return qs
}

// CreatePublicOrder runs the SECURITY DEFINER function that creates the
// customer (if new) and the order atomically.
func (r *PublicOrderRepo) CreatePublicOrder(ctx context.Context, in app.PublicOrderInput) (*app.PublicOrderResult, error) {
	var out *app.PublicOrderResult
	err := r.db.WithTx(ctx, func(tx pgx.Tx) error {
		const q = `
			SELECT * FROM create_public_order(
				$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12::jsonb
			)
		`
		orderID := r.ids.New()
		customerID := r.ids.New()
		auditID := r.ids.New()

		items, err := marshalPublicItems(r.ids, in.Items)
		if err != nil {
			return err
		}
		answers, err := marshalPublicAnswers(in.Answers)
		if err != nil {
			return err
		}

		var res app.PublicOrderResult
		if err := tx.QueryRow(ctx, q,
			in.Slug, in.CustomerName, in.CustomerEmail, in.CustomerPhone,
			in.Description, in.ExpectedDate, in.BudgetMinor,
			items,
			orderID, customerID, auditID,
			answers,
		).Scan(&res.OrderID, &res.OrderNumber, &res.OrgName, &res.OrgPhone, &res.TotalMinor, &res.Currency); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return app.ErrPublicOrgNotFound
			}
			return fmt.Errorf("public_order_repo: create: %w", err)
		}
		res.CustomerName = in.CustomerName
		out = &res
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil || out.OrderID == uuid.Nil {
		return nil, app.ErrPublicOrgNotFound
	}
	return out, nil
}

// marshalPublicAnswers encodes product-question answers for
// create_public_order: [{product, q, a}]. Empty yields '[]'.
func marshalPublicAnswers(in []app.PublicAnswer) (string, error) {
	type answer struct {
		Product string `json:"product"`
		Q       string `json:"q"`
		A       string `json:"a"`
	}
	out := make([]answer, 0, len(in))
	for _, a := range in {
		out = append(out, answer{Product: a.Product, Q: a.Q, A: a.A})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("public_order_repo: marshal answers: %w", err)
	}
	return string(raw), nil
}

// marshalPublicItems encodes product lines for create_public_order:
// [{id, product_id, qty}] with qty as an exact 3-decimal numeric string.
// Item ids are generated here so the function stays free of extension
// dependencies.
func marshalPublicItems(ids app.IDGen, items []app.PublicOrderItemInput) (string, error) {
	type line struct {
		ID        string `json:"id"`
		ProductID string `json:"product_id"`
		Qty       string `json:"qty"`
	}
	out := make([]line, 0, len(items))
	for _, it := range items {
		out = append(out, line{
			ID:        ids.New().String(),
			ProductID: it.ProductID.String(),
			Qty:       fmt.Sprintf("%.3f", float64(it.QuantityScaled)/1000.0),
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("public_order_repo: marshal items: %w", err)
	}
	return string(raw), nil
}
