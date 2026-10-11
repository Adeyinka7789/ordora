package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/payment"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// Perishable-goods module: daily capacity cap + transfer-receipt ordering.
// Egg orders ARE orders — statuses map onto order.Status, refunds use
// payment reversals. Only the cap counter and seller settings are new.

// DefaultMaxCrates is the cap for sellers who never configured one.
const DefaultMaxCrates = 100

// PoultrySettings is one row of poultry_settings.
type PoultrySettings struct {
	OrgID           uuid.UUID
	MaxCratesPerDay int
	AcceptingOrders bool
	BankName        string
	AccountNumber   string
	AccountName     string
	Whatsapp        string
	UpdatedAt       time.Time
}

// DefaultPoultrySettings returns the out-of-box settings for an org.
func DefaultPoultrySettings(orgID uuid.UUID) PoultrySettings {
	return PoultrySettings{OrgID: orgID, MaxCratesPerDay: DefaultMaxCrates, AcceptingOrders: true}
}

// PerishableConfig is the anonymous intake configuration for one slug.
type PerishableConfig struct {
	OrgID         uuid.UUID
	IsPerishable  bool
	FlagOn        bool
	Accepting     bool
	Cap           int
	SoldToday     float64
	SoldTomorrow  float64
	BankName      string
	AccountNumber string
	AccountName   string
	Whatsapp      string
	Timezone      string
	Currency      string
}

// EggApplies reports whether the egg intake UI should render.
func (c *PerishableConfig) EggApplies() bool {
	return c != nil && c.IsPerishable && c.FlagOn
}

// EggPanelOrder is one row of the seller's egg panel.
type EggPanelOrder struct {
	ID            uuid.UUID
	Number        string
	Status        string
	TotalMinor    int64
	PaidMinor     int64
	Currency      string
	CreatedAt     time.Time
	CustomerName  string
	CustomerPhone string
	QtyCrates     float64
	ReceiptID     *uuid.UUID
}

// EggOrderSummary is the small read confirm/reject need.
type EggOrderSummary struct {
	ID           uuid.UUID
	Number       string
	Status       string
	TotalMinor   int64
	PaidMinor    int64
	Currency     string
	ExpectedDate *time.Time
	CustomerID   uuid.UUID
	CreatedAt    time.Time
}

// Balance returns the outstanding minor units.
func (s *EggOrderSummary) Balance() int64 { return s.TotalMinor - s.PaidMinor }

// Errors.
var (
	ErrEggNotOwner        = errors.New("egg: only the business owner can do that")
	ErrEggClosed          = errors.New("egg: orders are closed")
	ErrEggBadStatus       = errors.New("egg: order is not in the right status for that")
	ErrEggReceiptRequired = errors.New("egg: a payment receipt photo is required")
	ErrEggNoItems         = errors.New("egg: pick at least one crate type")
	ErrEggSettingsInvalid = errors.New("egg: settings are invalid")
	ErrEggOrderNotFound   = errors.New("egg: order not found")
)

// EggSoldOutError carries how many crates are left for the day label.
type EggSoldOutError struct {
	Left     float64
	DayLabel string // "today" or "tomorrow"
}

func (e *EggSoldOutError) Error() string {
	return fmt.Sprintf("egg: only %s crates left for %s", FormatCrates(e.Left), e.DayLabel)
}

// FormatCrates renders a crate count without trailing zeros ("67", "2.5").
func FormatCrates(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// EggStore persists egg settings/counters and panel reads.
type EggStore interface {
	GetSettings(ctx context.Context, scope tenant.TenantScope) (*PoultrySettings, error)
	UpsertSettings(ctx context.Context, scope tenant.TenantScope, s *PoultrySettings) error
	SetAccepting(ctx context.Context, scope tenant.TenantScope, accepting bool, now time.Time) error
	GetSold(ctx context.Context, scope tenant.TenantScope, day time.Time) (float64, error)
	ReleaseCap(ctx context.Context, scope tenant.TenantScope, day time.Time, qty float64) error
	OrderCrates(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (float64, time.Time, error)
	OrderSummary(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) (*EggOrderSummary, error)
	ListPanelOrders(ctx context.Context, scope tenant.TenantScope, day time.Time) ([]EggPanelOrder, error)
}

// EggService orchestrates the perishable module. Staff (any writer role)
// confirm/reject/complete — that is the point (sales girl covers the owner).
// Settings stay owner-only.
type EggService struct {
	db       postgresDB
	store    EggStore
	payments *PaymentService
	orders   *OrderService
	ids      IDGen
	now      func() time.Time
}

type EggServiceDeps struct {
	DB       postgresDB
	Store    EggStore
	Payments *PaymentService
	Orders   *OrderService
	IDs      IDGen
	Now      func() time.Time
}

func NewEggService(d EggServiceDeps) *EggService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &EggService{
		db: d.DB, store: d.Store, payments: d.Payments,
		orders: d.Orders, ids: d.IDs, now: d.Now,
	}
}

// EggTimezone resolves the org timezone, falling back to Lagos.
func EggTimezone(tz string) *time.Location {
	if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil && tz != "" {
		return loc
	}
	loc, _ := time.LoadLocation("Africa/Lagos")
	if loc == nil {
		return time.UTC
	}
	return loc
}

// DeliveryDay maps "today"|"tomorrow" to a date in the org timezone.
func DeliveryDay(tz, day string, now time.Time) (time.Time, error) {
	inTZ := now.In(EggTimezone(tz))
	y, m, d := inTZ.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, inTZ.Location())
	switch strings.ToLower(strings.TrimSpace(day)) {
	case "today", "":
		return midnight, nil
	case "tomorrow":
		return midnight.Add(24 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("egg: unknown delivery day %q", day)
	}
}

// UpdateSettingsInput carries the owner-editable settings form.
type UpdateSettingsInput struct {
	MaxCratesPerDay int
	AcceptingOrders bool
	BankName        string
	AccountNumber   string
	AccountName     string
	Whatsapp        string
}

// GetSettings loads settings (defaults when never saved).
func (s *EggService) GetSettings(ctx context.Context, scope tenant.TenantScope) (*PoultrySettings, error) {
	return s.store.GetSettings(ctx, scope)
}

// UpdateSettings saves settings. Owner only: bank details and the cap are
// business-critical and staff must never touch them.
func (s *EggService) UpdateSettings(ctx context.Context, scope tenant.TenantScope, in UpdateSettingsInput) (*PoultrySettings, error) {
	if scope.Role != tenant.RoleOwner {
		return nil, ErrEggNotOwner
	}
	if in.MaxCratesPerDay <= 0 || in.MaxCratesPerDay > 100000 {
		return nil, ErrEggSettingsInvalid
	}
	now := s.now()
	out := &PoultrySettings{
		OrgID:           scope.OrgID,
		MaxCratesPerDay: in.MaxCratesPerDay,
		AcceptingOrders: in.AcceptingOrders,
		BankName:        strings.TrimSpace(in.BankName),
		AccountNumber:   strings.TrimSpace(in.AccountNumber),
		AccountName:     strings.TrimSpace(in.AccountName),
		Whatsapp:        strings.TrimSpace(in.Whatsapp),
		UpdatedAt:       now,
	}
	if len(out.BankName) > 120 || len(out.AccountNumber) > 40 ||
		len(out.AccountName) > 120 || len(out.Whatsapp) > 40 {
		return nil, ErrEggSettingsInvalid
	}
	if err := s.store.UpsertSettings(ctx, scope, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetAccepting flips the open/close toggle. Owner only.
func (s *EggService) SetAccepting(ctx context.Context, scope tenant.TenantScope, accepting bool) error {
	if scope.Role != tenant.RoleOwner {
		return ErrEggNotOwner
	}
	return s.store.SetAccepting(ctx, scope, accepting, s.now())
}

// PanelDay resolves the panel's day filter ("today"|"tomorrow").
func (s *EggService) PanelDay(tz, day string) (time.Time, string) {
	d, err := DeliveryDay(tz, day, s.now())
	if err != nil {
		d, _ = DeliveryDay(tz, "today", s.now())
		return d, "today"
	}
	if strings.ToLower(strings.TrimSpace(day)) == "tomorrow" {
		return d, "tomorrow"
	}
	return d, "today"
}

// PanelData is everything the seller panel needs.
type PanelData struct {
	Day       time.Time
	DayLabel  string // "today" | "tomorrow"
	Cap       int
	Sold      float64
	Orders    []EggPanelOrder
	Pending   int
	IsOwner   bool
	Accepting bool
}

// Panel loads one delivery day: cap, sold counter, orders.
func (s *EggService) Panel(ctx context.Context, scope tenant.TenantScope, tz, day string, pendingOnly bool) (*PanelData, error) {
	d, label := s.PanelDay(tz, day)
	settings, err := s.store.GetSettings(ctx, scope)
	if err != nil {
		return nil, err
	}
	sold, err := s.store.GetSold(ctx, scope, d)
	if err != nil {
		return nil, err
	}
	orders, err := s.store.ListPanelOrders(ctx, scope, d)
	if err != nil {
		return nil, err
	}
	out := &PanelData{
		Day: d, DayLabel: label,
		Cap: settings.MaxCratesPerDay, Sold: sold,
		IsOwner:   scope.Role == tenant.RoleOwner,
		Accepting: settings.AcceptingOrders,
	}
	for _, o := range orders {
		if pendingOnly && o.Status != string(order.StatusNew) {
			continue
		}
		out.Orders = append(out.Orders, o)
		if o.Status == string(order.StatusNew) {
			out.Pending++
		}
	}
	return out, nil
}

// ConfirmOrder records the transfer payment and confirms the order, in one
// seller click. Any writer role (owner or staff).
func (s *EggService) ConfirmOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) error {
	if err := scope.RequireWrite(); err != nil {
		return err
	}
	sum, err := s.store.OrderSummary(ctx, scope, orderID)
	if err != nil {
		return err
	}
	if order.Status(sum.Status) != order.StatusNew {
		return ErrEggBadStatus
	}
	now := s.now()
	if sum.Balance() > 0 {
		if _, err := s.payments.RecordPayment(ctx, scope, RecordPaymentInput{
			OrderID:   orderID,
			Amount:    sum.Balance(),
			Method:    payment.MethodBankTransfer,
			Reference: "Bank transfer (receipt)",
			PaidAt:    now,
			Notes:     "Confirmed against customer receipt photo.",
		}); err != nil {
			return err
		}
	}
	_, err = s.orders.ChangeStatus(ctx, scope, orderID, order.StatusConfirmed)
	return err
}

// RejectOrder cancels the order, releases its cap, and reverses any payment
// (the actual refund happens offline; the reference goes in the reason).
func (s *EggService) RejectOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID, refundRef string) error {
	if err := scope.RequireWrite(); err != nil {
		return err
	}
	sum, err := s.store.OrderSummary(ctx, scope, orderID)
	if err != nil {
		return err
	}
	st := order.Status(sum.Status)
	if st != order.StatusNew && st != order.StatusConfirmed {
		return ErrEggBadStatus
	}
	reason := "Out of stock — customer to be refunded offline."
	if ref := strings.TrimSpace(refundRef); ref != "" {
		if len(ref) > 120 {
			ref = ref[:120]
		}
		reason += " Ref: " + ref
	}
	// Reverse first: a reversal on a cancelled order may be refused, and a
	// recorded payment must never survive a rejection.
	payments, err := s.payments.ListForOrder(ctx, scope, orderID)
	if err != nil {
		return err
	}
	for _, p := range payments {
		if p.IsReversal() || p.IsReversed() {
			continue
		}
		if _, err := s.payments.ReversePayment(ctx, scope, ReverseInput{
			PaymentID: p.ID,
			Reason:    reason,
		}); err != nil && !errors.Is(err, ErrPaymentAlreadyReversed) {
			return err
		}
	}
	if _, err := s.orders.ChangeStatus(ctx, scope, orderID, order.StatusCancelled); err != nil {
		return err
	}
	qty, day, err := s.store.OrderCrates(ctx, scope, orderID)
	if err != nil {
		return err
	}
	if !day.IsZero() && qty > 0 {
		if err := s.store.ReleaseCap(ctx, scope, day, qty); err != nil {
			return err
		}
	}
	return nil
}

// eggCompleteSteps walks an in-flight order to delivered.
var eggCompleteSteps = []order.Status{
	order.StatusInProgress,
	order.StatusReady,
	order.StatusOutForDelivery,
	order.StatusDelivered,
	order.StatusCompleted,
}

// CompleteOrder walks a confirmed order through to COMPLETED after delivery.
func (s *EggService) CompleteOrder(ctx context.Context, scope tenant.TenantScope, orderID uuid.UUID) error {
	if err := scope.RequireWrite(); err != nil {
		return err
	}
	sum, err := s.store.OrderSummary(ctx, scope, orderID)
	if err != nil {
		return err
	}
	if order.Status(sum.Status) != order.StatusConfirmed {
		return ErrEggBadStatus
	}
	for _, next := range eggCompleteSteps {
		if _, err := s.orders.ChangeStatus(ctx, scope, orderID, next); err != nil {
			return err
		}
	}
	return nil
}
