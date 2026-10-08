package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/portal"
)

// PortalRepoStore is the read contract for the public portal.
type PortalRepoStore interface {
	GetByTokenHash(ctx context.Context, tokenHash []byte) (*portal.Order, error)
	ListItems(ctx context.Context, orderID uuid.UUID) ([]portal.Item, error)
	ListPayments(ctx context.Context, orderID uuid.UUID) ([]portal.Payment, error)
	GetPortalAttachment(ctx context.Context, tokenHash []byte, attachmentID uuid.UUID) (*PortalAttachment, error)
	GetPublicProductImage(ctx context.Context, attachmentID uuid.UUID) (*PublicProductImage, error)
}

// PortalAttachment is the token-authorized metadata for one proof file.
type PortalAttachment struct {
	ID         uuid.UUID
	OrderID    uuid.UUID
	OrgID      uuid.UUID
	Filename   string
	MimeType   string
	SizeBytes  int64
	StorageKey string
}

// PublicProductImage is the metadata for one public catalog image.
type PublicProductImage struct {
	StorageKey string
	MimeType   string
	Filename   string
	SizeBytes  int64
}

// BlobStore is the byte source for portal file downloads.
type BlobStore interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// PortalService serves the public customer-facing order view.
type PortalService struct {
	repo  PortalRepoStore
	blobs BlobStore
}

type PortalServiceDeps struct {
	Repo  PortalRepoStore
	Blobs BlobStore // optional; nil disables file downloads
}

func NewPortalService(d PortalServiceDeps) *PortalService {
	return &PortalService{repo: d.Repo, blobs: d.Blobs}
}

// PortalView is the projection the templates consume.
type PortalView struct {
	OrderID            uuid.UUID
	OrganizationID     uuid.UUID
	OrderNumber        string
	Title              string
	Description        string
	Status             order.Status
	StatusLabel        string
	Currency           string
	Subtotal           money.Money
	Discount           money.Money
	Tax                money.Money
	Total              money.Money
	Paid               money.Money
	Balance            money.Money
	PayStatus          payment.Status
	PaidPercent        int
	ExpectedCompletion *time.Time
	DeliveredAt        *time.Time
	CreatedAt          time.Time

	Items []PortalItemView

	CustomerName    string
	CustomerEmail   string
	CustomerPhone   string
	CustomerAddress string

	Payments []PortalPaymentView

	OrgName    string
	OrgSlug    string
	OrgEmail   string
	OrgPhone   string
	OrgAddress string
}

// PortalItemView is the display shape of a single line item in the portal.
// Material/ImageRef are order-time snapshots.
type PortalItemView struct {
	Description string
	Quantity    int64 // scaled by 1000, like order items
	UnitPrice   money.Money
	Subtotal    money.Money
	Material    string
	ImageRef    string // product cover attachment id at order time (may be "")
}

// PortalProof is one downloadable payment-proof file on the portal.
type PortalProof struct {
	ID       uuid.UUID
	Filename string
}

// PortalPaymentView is one payment line on the public receipt.
type PortalPaymentView struct {
	Method      string
	MethodLabel string
	Reference   string
	PaidAt      time.Time
	Amount      money.Money
	Notes       string
	IsReversed  bool
	IsReversal  bool
	ProofCount  int
	ProofNames  string
	Proofs      []PortalProof // aligned with ProofNames order
}

// ErrPortalNotFound is returned when a token is invalid, revoked, or unknown.
var ErrPortalNotFound = errors.New("portal: not found")

// ErrPortalFileUnavailable is returned when downloads are not configured
// or the backing bytes are missing.
var ErrPortalFileUnavailable = errors.New("portal: file unavailable")

// PortalFile is one served file: metadata for headers plus the byte stream.
// The caller must close Body.
type PortalFile struct {
	Filename  string
	MimeType  string
	SizeBytes int64
	Body      io.ReadCloser
}

// LoadAttachment resolves one customer-visible proof through the portal
// token and opens its bytes. The repo function enforces token validity,
// org match, order/payment ownership, and purpose; a nil result is a 404
// with no existence signal.
func (s *PortalService) LoadAttachment(ctx context.Context, rawToken string, attachmentID uuid.UUID) (*PortalFile, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || attachmentID == uuid.Nil {
		return nil, ErrPortalNotFound
	}
	if s.blobs == nil {
		return nil, ErrPortalFileUnavailable
	}
	a, err := s.repo.GetPortalAttachment(ctx, HashToken(rawToken), attachmentID)
	if err != nil {
		return nil, err
	}
	body, err := s.blobs.Get(ctx, a.StorageKey)
	if err != nil {
		return nil, ErrPortalFileUnavailable
	}
	return &PortalFile{
		Filename:  a.Filename,
		MimeType:  a.MimeType,
		SizeBytes: a.SizeBytes,
		Body:      body,
	}, nil
}

// LoadProductImage resolves one public catalog image and opens its bytes.
func (s *PortalService) LoadProductImage(ctx context.Context, attachmentID uuid.UUID) (*PortalFile, error) {
	if attachmentID == uuid.Nil {
		return nil, ErrPortalNotFound
	}
	if s.blobs == nil {
		return nil, ErrPortalFileUnavailable
	}
	img, err := s.repo.GetPublicProductImage(ctx, attachmentID)
	if err != nil {
		return nil, err
	}
	body, err := s.blobs.Get(ctx, img.StorageKey)
	if err != nil {
		return nil, ErrPortalFileUnavailable
	}
	return &PortalFile{
		Filename:  img.Filename,
		MimeType:  img.MimeType,
		SizeBytes: img.SizeBytes,
		Body:      body,
	}, nil
}

func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func HashTokenHex(raw string) string {
	return hex.EncodeToString(HashToken(raw))
}

// splitProofs aligns comma-joined proof ids with filenames (both ordered by
// created_at in get_public_order_payments). Malformed ids are dropped.
func splitProofs(ids, names string) []PortalProof {
	if strings.TrimSpace(ids) == "" {
		return nil
	}
	idParts := strings.Split(ids, ",")
	nameParts := strings.Split(names, ",")
	out := make([]PortalProof, 0, len(idParts))
	for i, raw := range idParts {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || id == uuid.Nil {
			continue
		}
		filename := ""
		if i < len(nameParts) {
			filename = strings.TrimSpace(nameParts[i])
		}
		out = append(out, PortalProof{ID: id, Filename: filename})
	}
	return out
}

func (s *PortalService) Load(ctx context.Context, rawToken string) (*PortalView, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrPortalNotFound
	}

	hash := HashToken(rawToken)
	po, err := s.repo.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrPortalNotFound) {
			return nil, ErrPortalNotFound
		}
		return nil, err
	}

	sub, _ := money.New(po.SubtotalMinor, po.Currency)
	dis, _ := money.New(po.DiscountMinor, po.Currency)
	tax, _ := money.New(po.TaxMinor, po.Currency)
	tot, _ := money.New(po.TotalMinor, po.Currency)
	paid, _ := money.New(po.AmountPaidMinor, po.Currency)
	bal, _ := tot.Sub(paid)

	status := order.Status(po.Status)
	payStatus := payment.DeriveStatus(tot, paid)

	var pct int
	if tot.Amount() > 0 {
		pct = int(paid.Amount() * 100 / tot.Amount())
	}

	// Load line items (best-effort; empty list is fine).
	itemRows, _ := s.repo.ListItems(ctx, po.OrderID)
	items := make([]PortalItemView, 0, len(itemRows))
	for _, it := range itemRows {
		up, _ := money.New(it.UnitPriceMinor, it.Currency)
		st, _ := money.New(it.SubtotalMinor, it.Currency)
		items = append(items, PortalItemView{
			Description: it.Description,
			Quantity:    int64(it.Quantity*1000 + 0.5), // scale to match internal representation
			UnitPrice:   up,
			Subtotal:    st,
			Material:    it.Material,
			ImageRef:    it.ImageRef,
		})
	}

	// Load payment lines for the receipt (best-effort; totals above already
	// carry paid/balance even when this is empty).
	payRows, _ := s.repo.ListPayments(ctx, po.OrderID)
	payments := make([]PortalPaymentView, 0, len(payRows))
	for _, p := range payRows {
		amt, _ := money.New(p.AmountMinor, p.Currency)
		payments = append(payments, PortalPaymentView{
			Method:      p.Method,
			MethodLabel: payment.Method(p.Method).Label(),
			Reference:   p.Reference,
			PaidAt:      p.PaidAt,
			Amount:      amt,
			Notes:       p.Notes,
			IsReversed:  p.IsReversed,
			IsReversal:  p.IsReversal,
			ProofCount:  p.ProofCount,
			ProofNames:  p.ProofNames,
			Proofs:      splitProofs(p.ProofIDs, p.ProofNames),
		})
	}

	return &PortalView{
		OrderID:            po.OrderID,
		OrganizationID:     po.OrganizationID,
		OrderNumber:        po.OrderNumber,
		Title:              po.Title,
		Description:        po.Description,
		Status:             status,
		StatusLabel:        status.Label(),
		Currency:           po.Currency,
		Subtotal:           sub,
		Discount:           dis,
		Tax:                tax,
		Total:              tot,
		Paid:               paid,
		Balance:            bal,
		PayStatus:          payStatus,
		PaidPercent:        pct,
		ExpectedCompletion: po.ExpectedCompletion,
		DeliveredAt:        po.DeliveredAt,
		CreatedAt:          po.CreatedAt,
		Items:              items,
		CustomerName:       po.CustomerName,
		CustomerEmail:      po.CustomerEmail,
		CustomerPhone:      po.CustomerPhone,
		CustomerAddress:    po.CustomerAddress,
		Payments:           payments,
		OrgName:            po.OrgName,
		OrgSlug:            po.OrgSlug,
		OrgEmail:           po.OrgEmail,
		OrgPhone:           po.OrgPhone,
		OrgAddress:         po.OrgAddress,
	}, nil
}
