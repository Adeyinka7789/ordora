package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/booking"
	"github.com/Adeyinka7789/ordora/internal/domain/order"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

// Appointment booking for service businesses (salons first). The order
// stays the commercial record; Booking is the time record linked 1:1.

// Booking statuses re-exported for callers that prefer app-level names.
const (
	BookingRequested = booking.StatusRequested
	BookingConfirmed = booking.StatusConfirmed
)

// BookingSettings is one row of booking_settings.
type BookingSettings struct {
	OrgID          uuid.UUID
	Accepting      bool
	DurationMin    int
	BufBefore      int
	BufAfter       int
	MinNoticeHours int
	MaxAdvanceDays int
	CancelPolicy   string
	UpdatedAt      time.Time
}

// DefaultBookingSettings returns out-of-box scheduling rules.
func DefaultBookingSettings(orgID uuid.UUID) BookingSettings {
	return BookingSettings{
		OrgID: orgID, Accepting: true, DurationMin: 60,
		MinNoticeHours: 2, MaxAdvanceDays: 30,
	}
}

// BookingHour is one weekday row (dow 0 = Sunday, minutes since midnight).
type BookingHour struct {
	Dow      int
	OpenMin  int
	CloseMin int
	IsOpen   bool
}

// DefaultWeekHours applies when the owner never saved hours:
// Mon-Sat 09:00-17:00, Sun closed.
func DefaultWeekHours() []BookingHour {
	out := make([]BookingHour, 0, 7)
	for dow := 0; dow <= 6; dow++ {
		h := BookingHour{Dow: dow, OpenMin: 540, CloseMin: 1020, IsOpen: dow != 0}
		out = append(out, h)
	}
	return out
}

// HoursFor returns the row for a weekday, defaulting when absent.
func HoursFor(hours []BookingHour, dow int) BookingHour {
	for _, h := range hours {
		if h.Dow == dow {
			return h
		}
	}
	for _, h := range DefaultWeekHours() {
		if h.Dow == dow {
			return h
		}
	}
	return BookingHour{Dow: dow, OpenMin: 540, CloseMin: 1020, IsOpen: dow != 0}
}

// BookingBlackout is one closed date.
type BookingBlackout struct {
	ID     uuid.UUID
	Day    time.Time
	Reason string
}

// Booking is an appointment with order/customer context for display.
type Booking struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	OrderID       uuid.UUID
	CustomerID    uuid.UUID
	ProductID     *uuid.UUID
	StaffUserID   *uuid.UUID
	StartAt       time.Time
	EndAt         time.Time
	Timezone      string
	BufBefore     int
	BufAfter      int
	Status        booking.Status
	CustomerNotes string
	CancelReason  string
	RescheduledAt *time.Time
	PrevStartAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// Joined context.
	OrderNumber   string
	OrderStatus   string
	CustomerName  string
	CustomerPhone string
	CustomerEmail string
	ServiceName   string
	TotalMinor    int64
	PaidMinor     int64
	Currency      string
	// StaffName is the assignee's display name ("" = anyone).
	StaffName string
}

// PublicBooking is the tracking-page projection of one appointment.
type PublicBooking struct {
	StartAt     time.Time
	EndAt       time.Time
	Timezone    string
	Status      string
	ServiceName string
	OrgName     string
	ProductID   *uuid.UUID
}

// BookingConfig is the anonymous booking configuration for one slug.
type BookingConfig struct {
	OrgID          uuid.UUID
	Eligible       bool
	FlagOn         bool
	Accepting      bool
	DurationMin    int
	BufBefore      int
	BufAfter       int
	MinNoticeHours int
	MaxAdvanceDays int
	CancelPolicy   string
	Timezone       string
	Currency       string
	OrgName        string
}

// BookingApplies reports whether the booking UI should render.
func (c *BookingConfig) BookingApplies() bool {
	return c != nil && c.Eligible && c.FlagOn
}

// BusyBlock is a live appointment blocking part of a day.
type BusyBlock struct {
	Start     time.Time
	End       time.Time
	BufBefore int
	BufAfter  int
	// StaffUserID is the assignee, or nil for shared-pool demand.
	StaffUserID *uuid.UUID
}

// BookingStaffMember is one bookable team member for the picker.
type BookingStaffMember struct {
	UserID uuid.UUID
	Name   string
}

// Slot is one bookable start/end pair (UTC instants).
type Slot struct {
	Start time.Time
	End   time.Time
}

// SlotGridStepMin is the picker granularity. Finer grids explode the
// button list; 30 minutes suits salon visits.
const SlotGridStepMin = 30

// Errors.
var (
	ErrBookingNotEligible   = errors.New("booking: business does not offer booking")
	ErrBookingClosed        = errors.New("booking: not accepting bookings")
	ErrBookingSlotTaken     = errors.New("booking: that time was just booked")
	ErrBookingBadSlot       = errors.New("booking: that time is not available")
	ErrBookingNotOwner      = errors.New("booking: only the business owner can do that")
	ErrBookingBadStatus     = errors.New("booking: booking is not in the right status for that")
	ErrBookingStale         = errors.New("booking: booking changed; please refresh")
	ErrBookingNotFound      = errors.New("booking: booking not found")
	ErrBookingNameRequired  = errors.New("booking: name is required")
	ErrBookingContactNeeded = errors.New("booking: email or phone is required")
	ErrBookingNoService     = errors.New("booking: choose a service")
	ErrBookingSettingsBad   = errors.New("booking: settings are invalid")
)

// BookingStore persists bookings and scheduling config.
type BookingStore interface {
	GetSettings(ctx context.Context, scope tenant.TenantScope) (*BookingSettings, error)
	UpsertSettings(ctx context.Context, scope tenant.TenantScope, s *BookingSettings) error
	SetHours(ctx context.Context, scope tenant.TenantScope, hours []BookingHour) error
	ListHours(ctx context.Context, scope tenant.TenantScope) ([]BookingHour, error)
	AddBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time, reason string, id uuid.UUID, now time.Time) error
	RemoveBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time) error
	ListBlackouts(ctx context.Context, scope tenant.TenantScope, from, to time.Time) ([]BookingBlackout, error)
	GetBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*Booking, error)
	ListDayBookings(ctx context.Context, scope tenant.TenantScope, fromUTC, toUTC time.Time) ([]Booking, error)
	UpdateBookingStatus(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, from, to booking.Status, cancelReason string, now time.Time) error
	Reschedule(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, newStart, newEnd time.Time, now time.Time) (*Booking, error)
	BookingConfig(ctx context.Context, slug string) (*BookingConfig, error)
	BookingHours(ctx context.Context, slug string) ([]BookingHour, error)
	BookingBlackouts(ctx context.Context, slug string, from, to time.Time) ([]BookingBlackout, error)
	BookingBusy(ctx context.Context, slug string, fromUTC, toUTC time.Time) ([]BusyBlock, error)
	CreateBooking(ctx context.Context, in CreateBookingInput) (*CreateBookingResult, error)
	BookingByToken(ctx context.Context, hash []byte) (*PublicBooking, error)
	BookingStaff(ctx context.Context, slug string) ([]BookingStaffMember, error)
}

// CreateBookingInput is the repo-level atomic creation payload. Times are
// absolute instants; Day scoping already happened upstream.
type CreateBookingInput struct {
	Slug          string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Notes         string
	ServiceName   string
	ProductID     *uuid.UUID
	Start         time.Time
	End           time.Time
	Timezone      string
	BufBefore     int
	BufAfter      int
	StaffUserID   *uuid.UUID
}

// CreateBookingResult carries the new rows plus the one-time raw token.
type CreateBookingResult struct {
	BookingID   uuid.UUID
	OrderID     uuid.UUID
	OrderNumber string
	RawToken    string
	TotalMinor  int64
	Currency    string
	OrgEmail    string
	Start       time.Time
	End         time.Time
}

// BookingService orchestrates appointments.
type BookingService struct {
	db     postgresDB
	store  BookingStore
	orders *OrderService
	outbox OutboxWriter
	ids    IDGen
	now    func() time.Time
	// baseURL builds absolute links for emails; portalSecret signs
	// customer tracking links. Empty disables the links (templates hide
	// the buttons).
	baseURL      string
	portalSecret string
}

type BookingServiceDeps struct {
	DB     postgresDB
	Store  BookingStore
	Orders *OrderService
	Outbox OutboxWriter
	IDs    IDGen
	Now    func() time.Time
	// BaseURL is the public app origin (cfg.BaseURL). PortalSecret signs
	// customer tracking links (cfg.PortalSecret).
	BaseURL      string
	PortalSecret string
}

func NewBookingService(d BookingServiceDeps) *BookingService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &BookingService{
		db: d.DB, store: d.Store, orders: d.Orders,
		outbox: d.Outbox, ids: d.IDs, now: d.Now,
		baseURL: d.BaseURL, portalSecret: d.PortalSecret,
	}
}

// BookingTimezone resolves the org timezone, falling back to Lagos.
func BookingTimezone(tz string) *time.Location {
	if loc, err := time.LoadLocation(strings.TrimSpace(tz)); err == nil && tz != "" {
		return loc
	}
	if loc, err := time.LoadLocation("Africa/Lagos"); err == nil {
		return loc
	}
	return time.UTC
}

// LocalMidnight returns midnight of d's calendar day in loc.
func LocalMidnight(d time.Time, loc *time.Location) time.Time {
	y, m, dd := d.In(loc).Date()
	return time.Date(y, m, dd, 0, 0, 0, 0, loc)
}

// AvailableSlots computes bookable starts for one local day. Pure and
// fully tested: the creation transaction re-validates authoritatively,
// so this list is advisory (a concurrent booking wins the race and the
// submitter gets "just booked").
//
// staff scopes the lane: a named pick ignores other stylists' visits
// (unassigned demand still blocks — it could land on anyone); nil keeps
// the conservative org-wide view.
func AvailableSlots(day time.Time, loc *time.Location, now time.Time, settings BookingSettings, hours []BookingHour, blackouts map[string]bool, busy []BusyBlock, durationMin int, staff *uuid.UUID) []Slot {
	if durationMin <= 0 {
		durationMin = settings.DurationMin
	}
	if durationMin <= 0 {
		durationMin = 60
	}
	mid := LocalMidnight(day, loc)
	y, m, d := mid.Date()
	dayKey := mid.Format("2006-01-02")
	_ = y
	_ = m
	_ = d
	if blackouts[dayKey] {
		return nil
	}
	dow := int(mid.Weekday())
	h := HoursFor(hours, dow)
	if !h.IsOpen || h.CloseMin <= h.OpenMin {
		return nil
	}
	open := mid.Add(time.Duration(h.OpenMin) * time.Minute)
	close := mid.Add(time.Duration(h.CloseMin) * time.Minute)
	lastStart := close.Add(-time.Duration(durationMin) * time.Minute)
	earliest := now.Add(time.Duration(settings.MinNoticeHours) * time.Hour)
	latestDay := LocalMidnight(now.AddDate(0, 0, settings.MaxAdvanceDays), loc)
	if mid.After(latestDay) {
		return nil
	}
	var out []Slot
	for s := open; !s.After(lastStart); s = s.Add(SlotGridStepMin * time.Minute) {
		e := s.Add(time.Duration(durationMin) * time.Minute)
		if s.Before(earliest) {
			continue
		}
		// Candidate blocked when its buffered window touches a live
		// booking's buffered window (buffers snapshot per booking) in
		// the same lane: same stylist, or unassigned demand either way.
		bs := s.Add(-time.Duration(settings.BufBefore) * time.Minute)
		be := e.Add(time.Duration(settings.BufAfter) * time.Minute)
		blocked := false
		for _, b := range busy {
			if staff != nil && b.StaffUserID != nil && *b.StaffUserID != *staff {
				continue
			}
			if bs.Before(b.End.Add(time.Duration(b.BufAfter)*time.Minute)) &&
				be.After(b.Start.Add(-time.Duration(b.BufBefore)*time.Minute)) {
				blocked = true
				break
			}
		}
		if !blocked {
			out = append(out, Slot{Start: s, End: e})
		}
	}
	return out
}

// UpdateBookingSettingsInput carries the owner-editable scheduling form.
type UpdateBookingSettingsInput struct {
	Accepting      bool
	DurationMin    int
	BufBefore      int
	BufAfter       int
	MinNoticeHours int
	MaxAdvanceDays int
	CancelPolicy   string
}

// GetSettings loads settings (defaults when never saved).
func (s *BookingService) GetSettings(ctx context.Context, scope tenant.TenantScope) (*BookingSettings, error) {
	return s.store.GetSettings(ctx, scope)
}

// UpdateSettings saves scheduling rules. Owner only.
func (s *BookingService) UpdateSettings(ctx context.Context, scope tenant.TenantScope, in UpdateBookingSettingsInput) (*BookingSettings, error) {
	if scope.Role != tenant.RoleOwner {
		return nil, ErrBookingNotOwner
	}
	if in.DurationMin < 15 || in.DurationMin > 480 ||
		in.BufBefore < 0 || in.BufBefore > 120 ||
		in.BufAfter < 0 || in.BufAfter > 120 ||
		in.MinNoticeHours < 0 || in.MinNoticeHours > 720 ||
		in.MaxAdvanceDays < 1 || in.MaxAdvanceDays > 365 {
		return nil, ErrBookingSettingsBad
	}
	if len(in.CancelPolicy) > 2000 {
		return nil, ErrBookingSettingsBad
	}
	out := &BookingSettings{
		OrgID: scope.OrgID, Accepting: in.Accepting,
		DurationMin: in.DurationMin, BufBefore: in.BufBefore, BufAfter: in.BufAfter,
		MinNoticeHours: in.MinNoticeHours, MaxAdvanceDays: in.MaxAdvanceDays,
		CancelPolicy: strings.TrimSpace(in.CancelPolicy),
		UpdatedAt:    s.now(),
	}
	if err := s.store.UpsertSettings(ctx, scope, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetHours validates and saves the weekly schedule. Owner only.
func (s *BookingService) SetHours(ctx context.Context, scope tenant.TenantScope, hours []BookingHour) error {
	if scope.Role != tenant.RoleOwner {
		return ErrBookingNotOwner
	}
	if len(hours) != 7 {
		return ErrBookingSettingsBad
	}
	seen := map[int]bool{}
	for _, h := range hours {
		if h.Dow < 0 || h.Dow > 6 || seen[h.Dow] {
			return ErrBookingSettingsBad
		}
		seen[h.Dow] = true
		if h.OpenMin < 0 || h.OpenMin > 1440 || h.CloseMin < 0 || h.CloseMin > 1440 {
			return ErrBookingSettingsBad
		}
		if h.IsOpen && h.CloseMin <= h.OpenMin {
			return ErrBookingSettingsBad
		}
	}
	return s.store.SetHours(ctx, scope, hours)
}

// AddBlackout closes one date. Owner only.
func (s *BookingService) AddBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time, reason string) error {
	if scope.Role != tenant.RoleOwner {
		return ErrBookingNotOwner
	}
	return s.store.AddBlackout(ctx, scope, day, reason, s.ids.New(), s.now())
}

// RemoveBlackout reopens one date. Owner only.
func (s *BookingService) RemoveBlackout(ctx context.Context, scope tenant.TenantScope, day time.Time) error {
	if scope.Role != tenant.RoleOwner {
		return ErrBookingNotOwner
	}
	return s.store.RemoveBlackout(ctx, scope, day)
}

// setStatus moves a booking and its linked order together. The linked
// order is read first and must sit in the expected state — staff acting
// directly in the orders UI can't silently drift the two records apart.
// Order steps run before the booking write; a mid-walk failure returns
// the error with nothing half-applied on the booking side.
func (s *BookingService) setStatus(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, to booking.Status, cancelReason string) (*Booking, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	b, err := s.store.GetBooking(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if !b.Status.CanTransitionTo(to) {
		return nil, ErrBookingBadStatus
	}
	o, err := s.orders.GetOrder(ctx, scope, b.OrderID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	switch to {
	case booking.StatusConfirmed:
		// Confirm pairs REQUESTED with a NEW order. Anything else means
		// someone already acted — refresh instead of guessing.
		if o.Status != order.StatusNew {
			return nil, ErrBookingBadStatus
		}
		if _, err := s.orders.ChangeStatus(ctx, scope, o.ID, order.StatusConfirmed); err != nil {
			return nil, err
		}
	case booking.StatusCompleted:
		// Walk the visit to done. Must start CONFIRMED (set by confirm).
		// Intermediate states move silently — one appointment completion
		// must not spam "ready", "out for delivery" and "delivered" for
		// a haircut. Only the final COMPLETED step mails the customer.
		if o.Status != order.StatusConfirmed {
			return nil, ErrBookingBadStatus
		}
		for _, next := range []order.Status{
			order.StatusInProgress, order.StatusReady,
			order.StatusOutForDelivery, order.StatusDelivered,
		} {
			if s.orders == nil {
				return nil, ErrBookingBadStatus
			}
			if _, err := s.orders.ChangeStatusSilent(ctx, scope, o.ID, next); err != nil {
				return nil, err
			}
		}
		if s.orders == nil {
			return nil, ErrBookingBadStatus
		}
		if _, err := s.orders.ChangeStatus(ctx, scope, o.ID, order.StatusCompleted); err != nil {
			return nil, err
		}
	case booking.StatusCancelled, booking.StatusNoShow:
		// Close the commercial record unless already closed. DELIVERED
		// orders can't cancel (state machine) — surface it, don't force it.
		switch o.Status {
		case order.StatusCompleted, order.StatusCancelled:
			// Already closed: booking still records the outcome.
		default:
			if _, err := s.orders.ChangeStatus(ctx, scope, o.ID, order.StatusCancelled); err != nil {
				return nil, err
			}
		}
	}
	if err := s.store.UpdateBookingStatus(ctx, scope, id, b.Status, to, cancelReason, now); err != nil {
		return nil, err
	}
	b.Status = to
	return b, nil
}

// ConfirmBooking accepts a requested visit. Any writer role. The customer
// mail comes from the linked order's NEW->CONFIRMED transition.
func (s *BookingService) ConfirmBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*Booking, error) {
	return s.setStatus(ctx, scope, id, booking.StatusConfirmed, "")
}

// CompleteBooking closes a confirmed visit after it happened. Customer
// mail comes from the order walk to COMPLETED.
func (s *BookingService) CompleteBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*Booking, error) {
	return s.setStatus(ctx, scope, id, booking.StatusCompleted, "")
}

// CancelBooking calls off a visit and frees its slot. Customer mail comes
// from the order's transition to CANCELLED.
func (s *BookingService) CancelBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, reason string) (*Booking, error) {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return s.setStatus(ctx, scope, id, booking.StatusCancelled, strings.TrimSpace(reason))
}

// NoShowBooking records a missed confirmed visit and frees its slot.
// Customer mail comes from the order's transition to CANCELLED.
func (s *BookingService) NoShowBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*Booking, error) {
	return s.setStatus(ctx, scope, id, booking.StatusNoShow, "")
}

// ---------------------------------------------------------------------------
// Public flow (no login; eligibility comes from the config function).
// ---------------------------------------------------------------------------

// SubmitBookingInput is the customer-facing booking request.
type SubmitBookingInput struct {
	Slug          string
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Notes         string
	ProductID     uuid.UUID
	ServiceName   string
	Date          string // yyyy-mm-dd, org-local
	StartTime     string // HH:MM, org-local, 24h
	// DurationMin resolves server-side from the service catalog (0 = org
	// default); the transaction enforces it against the service row.
	DurationMin int
	// StaffUserID is the picked stylist, or nil for anyone available.
	StaffUserID *uuid.UUID
}

// SubmitBookingResult is the confirmation payload.
type SubmitBookingResult struct {
	BookingID    uuid.UUID
	OrderID      uuid.UUID
	OrderNumber  string
	RawToken     string
	TotalMinor   int64
	Currency     string
	OrgName      string
	Start        time.Time
	End          time.Time
	CustomerName string
}

// parseBookingStart resolves date + HH:MM in the org timezone.
func parseBookingStart(tz, dateStr, timeStr string) (time.Time, error) {
	loc := BookingTimezone(tz)
	if strings.TrimSpace(dateStr) == "" || strings.TrimSpace(timeStr) == "" {
		return time.Time{}, ErrBookingBadSlot
	}
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), loc)
	if err != nil {
		return time.Time{}, ErrBookingBadSlot
	}
	hm, err := time.ParseInLocation("15:04", strings.TrimSpace(timeStr), loc)
	if err != nil {
		return time.Time{}, ErrBookingBadSlot
	}
	y, m, d := day.Date()
	return time.Date(y, m, d, hm.Hour(), hm.Minute(), 0, 0, loc), nil
}

// dayBounds returns the UTC window covering one org-local date.
func dayBounds(tz string, day time.Time) (time.Time, time.Time) {
	loc := BookingTimezone(tz)
	mid := LocalMidnight(day, loc)
	return mid.UTC(), mid.Add(24 * time.Hour).UTC()
}

// GetBooking loads one booking with order/customer context.
func (s *BookingService) GetBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID) (*Booking, error) {
	return s.store.GetBooking(ctx, scope, id)
}

// SettingsPageData bundles settings + hours + upcoming blackouts for the
// settings screen.
type SettingsPageData struct {
	Settings  *BookingSettings
	Hours     []BookingHour
	Blackouts []BookingBlackout
}

// SettingsPage loads the settings screen data (90 days of blackouts).
func (s *BookingService) SettingsPage(ctx context.Context, scope tenant.TenantScope) (*SettingsPageData, error) {
	settings, err := s.store.GetSettings(ctx, scope)
	if err != nil {
		return nil, err
	}
	hours, err := s.store.ListHours(ctx, scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	blackouts, err := s.store.ListBlackouts(ctx, scope, now.Add(-24*time.Hour), now.AddDate(0, 0, 90))
	if err != nil {
		return nil, err
	}
	return &SettingsPageData{Settings: settings, Hours: hours, Blackouts: blackouts}, nil
}

// PublicConfig returns the anonymous booking configuration for a slug.
// Unknown slugs read as ineligible; callers 404.
func (s *BookingService) PublicConfig(ctx context.Context, slug string) (*BookingConfig, error) {
	return s.store.BookingConfig(ctx, slug)
}

// PublicStaff lists bookable team members for a slug. Empty when none
// (solo shops just don't offer the picker).
func (s *BookingService) PublicStaff(ctx context.Context, slug string) ([]BookingStaffMember, error) {
	return s.store.BookingStaff(ctx, slug)
}

// DaySlots is the slot-picker payload for one date.
type DaySlots struct {
	Config    *BookingConfig
	Date      string // echo, yyyy-mm-dd
	Slots     []Slot
	Hours     BookingHour
	Closed    bool   // Sunday/blackout/after-hours day
	ClosedWhy string // "sunday" | "blackout" | ""
}

// SlotsForDate computes the advisory slot grid for a public date.
func (s *BookingService) SlotsForDate(ctx context.Context, slug, dateStr string, durationMin int, staff *uuid.UUID) (*DaySlots, error) {
	cfg, err := s.store.BookingConfig(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !cfg.BookingApplies() {
		return nil, ErrBookingNotEligible
	}
	loc := BookingTimezone(cfg.Timezone)
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), loc)
	if err != nil {
		return nil, ErrBookingBadSlot
	}
	day = LocalMidnight(day, loc)
	now := s.now()

	out := &DaySlots{Config: cfg, Date: day.Format("2006-01-02")}
	// Window guard first (cheap, friendly).
	latest := LocalMidnight(now.AddDate(0, 0, cfg.MaxAdvanceDays), loc)
	if day.After(latest) {
		return nil, ErrBookingBadSlot
	}
	hours, err := s.store.BookingHours(ctx, slug)
	if err != nil {
		return nil, err
	}
	blackouts, err := s.store.BookingBlackouts(ctx, slug, day, day)
	if err != nil {
		return nil, err
	}
	black := map[string]bool{}
	for _, b := range blackouts {
		black[b.Day.Format("2006-01-02")] = true
	}
	out.Hours = HoursFor(hours, int(day.Weekday()))
	if black[out.Date] {
		out.Closed, out.ClosedWhy = true, "blackout"
		return out, nil
	}
	if !out.Hours.IsOpen {
		out.Closed, out.ClosedWhy = true, "closed"
		return out, nil
	}
	fromUTC, toUTC := dayBounds(cfg.Timezone, day)
	busy, err := s.store.BookingBusy(ctx, slug, fromUTC, toUTC)
	if err != nil {
		return nil, err
	}
	settings := BookingSettings{
		DurationMin: cfg.DurationMin, BufBefore: cfg.BufBefore, BufAfter: cfg.BufAfter,
		MinNoticeHours: cfg.MinNoticeHours, MaxAdvanceDays: cfg.MaxAdvanceDays,
	}
	dur := durationMin
	if dur <= 0 {
		dur = cfg.DurationMin
	}
	if dur < 15 || dur > 480 {
		return nil, ErrBookingBadSlot
	}
	out.Slots = AvailableSlots(day, loc, now, settings, hours, black, busy, dur, staff)
	return out, nil
}

// SubmitBooking validates a request with friendly errors, then creates it
// atomically (the transaction re-validates everything; a concurrent win
// surfaces as ErrBookingSlotTaken).
func (s *BookingService) SubmitBooking(ctx context.Context, in SubmitBookingInput) (*SubmitBookingResult, error) {
	name := strings.TrimSpace(in.CustomerName)
	email := strings.TrimSpace(strings.ToLower(in.CustomerEmail))
	phone := strings.TrimSpace(in.CustomerPhone)
	if name == "" {
		return nil, ErrBookingNameRequired
	}
	if email == "" && phone == "" {
		return nil, ErrBookingContactNeeded
	}
	if in.ProductID == uuid.Nil || strings.TrimSpace(in.ServiceName) == "" {
		return nil, ErrBookingNoService
	}
	cfg, err := s.store.BookingConfig(ctx, in.Slug)
	if err != nil {
		return nil, err
	}
	if !cfg.BookingApplies() {
		return nil, ErrBookingNotEligible
	}
	if !cfg.Accepting {
		return nil, ErrBookingClosed
	}
	start, err := parseBookingStart(cfg.Timezone, in.Date, in.StartTime)
	if err != nil {
		return nil, err
	}
	dur := in.DurationMin
	if dur <= 0 {
		dur = cfg.DurationMin
	}
	if dur < 15 || dur > 480 {
		return nil, ErrBookingBadSlot
	}
	end := start.Add(time.Duration(dur) * time.Minute)

	// Friendly pre-checks (advisory; the tx decides).
	daySlots, err := s.SlotsForDate(ctx, in.Slug, in.Date, dur, in.StaffUserID)
	if err != nil {
		return nil, err
	}
	if daySlots.Closed {
		return nil, ErrBookingBadSlot
	}
	found := false
	for _, sl := range daySlots.Slots {
		if sl.Start.Equal(start) && sl.End.Equal(end) {
			found = true
			break
		}
	}
	if !found {
		// Either taken since render or never valid — the tx will tell
		// us exactly which; assume taken for the friendlier message.
		return nil, ErrBookingSlotTaken
	}

	notes := strings.TrimSpace(in.Notes)
	if len(notes) > 2000 {
		notes = notes[:2000]
	}
	pid := in.ProductID
	res, err := s.store.CreateBooking(ctx, CreateBookingInput{
		Slug: in.Slug, CustomerName: name, CustomerEmail: email,
		CustomerPhone: phone, Notes: notes, ServiceName: strings.TrimSpace(in.ServiceName),
		ProductID: &pid, Start: start, End: end, Timezone: cfg.Timezone,
		BufBefore: cfg.BufBefore, BufAfter: cfg.BufAfter,
		StaffUserID: in.StaffUserID,
	})
	if err != nil {
		return nil, err
	}
	out := &SubmitBookingResult{
		BookingID: res.BookingID, OrderID: res.OrderID,
		OrderNumber: res.OrderNumber, RawToken: res.RawToken,
		TotalMinor: res.TotalMinor, Currency: res.Currency,
		OrgName: cfg.OrgName, Start: start, End: end, CustomerName: name,
	}
	// Owner learns of the request (the intake_received template shape is
	// filled exactly — the renderer errors on missing keys).
	if s.outbox != nil && res.OrgEmail != "" {
		enqueueRetry(ctx, s.outbox, cfg.OrgID, "booking.submitted", map[string]any{
			"OrderID":       res.OrderID.String(),
			"OrderNumber":   res.OrderNumber,
			"CustomerName":  name,
			"CustomerEmail": email,
			"CustomerPhone": phone,
			"Description":   "Booking: " + strings.TrimSpace(in.ServiceName) + " — " + start.Format("Mon 02 Jan, 15:04"),
			"OrgName":       cfg.OrgName,
			"OrgEmail":      res.OrgEmail,
			"AdminURL":      AbsURL(s.baseURL, "/dashboard/bookings?date="+start.Format("2006-01-02")),
		}, "order_id", res.OrderID.String(), "org_id", cfg.OrgID.String())
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Staff panel.
// ---------------------------------------------------------------------------

// RescheduleBooking moves a live visit after re-checking conflicts.
// The rules below mirror the public submit path so staff get friendly
// errors up front; the repo re-enforces everything inside the locked
// transaction (a concurrent settings edit can't slip a bad move in).
func (s *BookingService) RescheduleBooking(ctx context.Context, scope tenant.TenantScope, id uuid.UUID, newStart, newEnd time.Time) (*Booking, error) {
	if err := scope.RequireWrite(); err != nil {
		return nil, err
	}
	if !newEnd.After(newStart) {
		return nil, ErrBookingBadSlot
	}
	b, err := s.store.GetBooking(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	if b.Status != booking.StatusRequested && b.Status != booking.StatusConfirmed {
		return nil, ErrBookingBadStatus
	}
	// A move keeps its duration; only the start shifts.
	if !newEnd.Equal(newStart.Add(b.EndAt.Sub(b.StartAt))) {
		return nil, ErrBookingBadSlot
	}
	if err := s.validateMove(ctx, scope, b.Timezone, newStart, newEnd); err != nil {
		return nil, err
	}
	b, err = s.store.Reschedule(ctx, scope, id, newStart, newEnd, s.now())
	if err != nil {
		return nil, err
	}
	s.BookingNotify(ctx, scope, "booking.rescheduled", b)
	return b, nil
}

// DayPanel is one calendar day of bookings.
type DayPanel struct {
	Day       time.Time
	DayLabel  string // yyyy-mm-dd
	Bookings  []Booking
	Requested int
}

// DayPanel loads bookings overlapping one org-local date.
func (s *BookingService) DayPanel(ctx context.Context, scope tenant.TenantScope, tz, dateStr string) (*DayPanel, error) {
	loc := BookingTimezone(tz)
	var day time.Time
	if strings.TrimSpace(dateStr) == "" {
		day = LocalMidnight(s.now(), loc)
	} else {
		d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateStr), loc)
		if err != nil {
			return nil, ErrBookingBadSlot
		}
		day = LocalMidnight(d, loc)
	}
	fromUTC, toUTC := dayBounds(tz, day)
	list, err := s.store.ListDayBookings(ctx, scope, fromUTC, toUTC)
	if err != nil {
		return nil, err
	}
	out := &DayPanel{Day: day, DayLabel: day.Format("2006-01-02"), Bookings: list}
	for _, b := range list {
		if b.Status == booking.StatusRequested {
			out.Requested++
		}
	}
	return out, nil
}

// BookingByToken resolves an appointment for the public tracking page.
func (s *BookingService) BookingByToken(ctx context.Context, rawToken string) (*PublicBooking, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, nil
	}
	return s.store.BookingByToken(ctx, HashToken(rawToken))
}

// validateMove checks a new appointment time against the current rules:
// future, inside notice/advance window, within weekly hours, not on a
// blackout. Advisory only — the repo enforces authoritatively in-tx.
func (s *BookingService) validateMove(ctx context.Context, scope tenant.TenantScope, tz string, newStart, newEnd time.Time) error {
	settings, err := s.store.GetSettings(ctx, scope)
	if err != nil {
		return err
	}
	loc := BookingTimezone(tz)
	now := s.now()
	if !newStart.After(now) ||
		newStart.Before(now.Add(time.Duration(settings.MinNoticeHours)*time.Hour)) {
		return ErrBookingBadSlot
	}
	latest := LocalMidnight(now.In(loc).AddDate(0, 0, settings.MaxAdvanceDays), loc)
	if !newStart.Before(latest.Add(24 * time.Hour)) {
		return ErrBookingBadSlot
	}
	hours, err := s.store.ListHours(ctx, scope)
	if err != nil {
		return err
	}
	h := HoursFor(hours, int(newStart.In(loc).Weekday()))
	if !h.IsOpen {
		return ErrBookingBadSlot
	}
	startMin := newStart.In(loc).Hour()*60 + newStart.In(loc).Minute()
	endMin := newEnd.In(loc).Hour()*60 + newEnd.In(loc).Minute()
	if startMin < h.OpenMin || endMin > h.CloseMin {
		return ErrBookingBadSlot
	}
	day := LocalMidnight(newStart, loc)
	blackouts, err := s.store.ListBlackouts(ctx, scope, day, day)
	if err != nil {
		return err
	}
	if len(blackouts) > 0 {
		return ErrBookingBadSlot
	}
	return nil
}

// BookingNotify enqueues the customer mail for a reschedule. Other staff
// actions deliberately enqueue nothing here: the linked order's own
// status transitions already mail the customer (order.status_changed),
// and a second mail per action would be a duplicate.
//
// Post-commit like the rest of the outbox usage, but retried and loud on
// failure instead of discarded: failures never fail the mutation, yet
// they no longer vanish silently.
func (s *BookingService) BookingNotify(ctx context.Context, scope tenant.TenantScope, event string, b *Booking) {
	if s.outbox == nil || b == nil || event != "booking.rescheduled" {
		return
	}
	if b.CustomerEmail == "" {
		return
	}
	loc := BookingTimezone(b.Timezone)
	enqueueRetry(ctx, s.outbox, scope.OrgID, event, map[string]any{
		"OrderID":       b.OrderID.String(),
		"OrderNumber":   b.OrderNumber,
		"CustomerName":  b.CustomerName,
		"CustomerEmail": b.CustomerEmail,
		"StartLabel":    b.StartAt.In(loc).Format("Mon 02 Jan, 15:04"),
		"PortalURL":     TrackingLink(s.baseURL, s.portalSecret, b.OrderID),
	}, "booking_id", b.ID.String(), "order_id", b.OrderID.String())
}
