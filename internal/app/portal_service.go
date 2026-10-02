package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
}

// PortalService serves the public customer-facing order view.
type PortalService struct {
	repo PortalRepoStore
}

type PortalServiceDeps struct {
	Repo PortalRepoStore
}

func NewPortalService(d PortalServiceDeps) *PortalService {
	return &PortalService{repo: d.Repo}
}

// PortalView is the projection the templates consume.
type PortalView struct {
	OrderID            uuid.UUID
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

	CustomerName  string
	CustomerEmail string
	CustomerPhone string

	OrgName    string
	OrgSlug    string
	OrgEmail   string
	OrgPhone   string
	OrgAddress string
}

// ErrPortalNotFound is returned when a token is invalid, revoked, or unknown.
var ErrPortalNotFound = errors.New("portal: not found")

func HashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func HashTokenHex(raw string) string {
	return hex.EncodeToString(HashToken(raw))
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

	return &PortalView{
		OrderID:            po.OrderID,
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
		CustomerName:       po.CustomerName,
		CustomerEmail:      po.CustomerEmail,
		CustomerPhone:      po.CustomerPhone,
		OrgName:            po.OrgName,
		OrgSlug:            po.OrgSlug,
		OrgEmail:           po.OrgEmail,
		OrgPhone:           po.OrgPhone,
		OrgAddress:         po.OrgAddress,
	}, nil
}
