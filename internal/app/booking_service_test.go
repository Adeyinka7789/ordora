package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/booking"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

type stubBookingStore struct {
	settings *BookingSettings
	saved    *BookingSettings
	hours    []BookingHour
	booking  *Booking
	moved    bool
}

func (s *stubBookingStore) GetSettings(_ context.Context, _ tenant.TenantScope) (*BookingSettings, error) {
	if s.settings != nil {
		return s.settings, nil
	}
	return &BookingSettings{Accepting: true, DurationMin: 60, MinNoticeHours: 2, MaxAdvanceDays: 30}, nil
}

func (s *stubBookingStore) UpsertSettings(_ context.Context, _ tenant.TenantScope, st *BookingSettings) error {
	s.saved = st
	return nil
}

func (s *stubBookingStore) SetHours(_ context.Context, _ tenant.TenantScope, h []BookingHour) error {
	s.hours = h
	return nil
}

func (s *stubBookingStore) ListHours(_ context.Context, _ tenant.TenantScope) ([]BookingHour, error) {
	return s.hours, nil
}

func (s *stubBookingStore) AddBlackout(_ context.Context, _ tenant.TenantScope, _ time.Time, _ string, _ uuid.UUID, _ time.Time) error {
	return nil
}

func (s *stubBookingStore) RemoveBlackout(_ context.Context, _ tenant.TenantScope, _ time.Time) error {
	return nil
}

func (s *stubBookingStore) ListBlackouts(_ context.Context, _ tenant.TenantScope, _, _ time.Time) ([]BookingBlackout, error) {
	return nil, nil
}

func (s *stubBookingStore) GetBooking(_ context.Context, _ tenant.TenantScope, _ uuid.UUID) (*Booking, error) {
	if s.booking != nil {
		return s.booking, nil
	}
	return nil, ErrBookingNotFound
}

func (s *stubBookingStore) ListDayBookings(_ context.Context, _ tenant.TenantScope, _, _ time.Time) ([]Booking, error) {
	return nil, nil
}

func (s *stubBookingStore) UpdateBookingStatus(_ context.Context, _ tenant.TenantScope, _ uuid.UUID, _, _ booking.Status, _ string, _ time.Time) error {
	return nil
}

func (s *stubBookingStore) Reschedule(_ context.Context, _ tenant.TenantScope, _ uuid.UUID, _, _ time.Time, _ time.Time) (*Booking, error) {
	s.moved = true
	return s.booking, nil
}

func (s *stubBookingStore) BookingConfig(_ context.Context, _ string) (*BookingConfig, error) {
	return &BookingConfig{}, nil
}

func (s *stubBookingStore) BookingHours(_ context.Context, _ string) ([]BookingHour, error) {
	return nil, nil
}

func (s *stubBookingStore) BookingBlackouts(_ context.Context, _ string, _, _ time.Time) ([]BookingBlackout, error) {
	return nil, nil
}

func (s *stubBookingStore) BookingBusy(_ context.Context, _ string, _, _ time.Time) ([]BusyBlock, error) {
	return nil, nil
}

func (s *stubBookingStore) CreateBooking(_ context.Context, _ CreateBookingInput) (*CreateBookingResult, error) {
	return nil, ErrBookingBadSlot
}

func (s *stubBookingStore) BookingByToken(_ context.Context, _ []byte) (*PublicBooking, error) {
	return nil, nil
}

func (s *stubBookingStore) BookingStaff(_ context.Context, _ string) ([]BookingStaffMember, error) {
	return nil, nil
}

func bookingTestScope(role tenant.Role) tenant.TenantScope {
	return tenant.TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: role}
}

func TestBookingUpdateSettings_Validation(t *testing.T) {
	store := &stubBookingStore{}
	svc := NewBookingService(BookingServiceDeps{Store: store})
	ctx := context.Background()
	owner := bookingTestScope(tenant.RoleOwner)

	if _, err := svc.UpdateSettings(ctx,
		bookingTestScope(tenant.RoleStaff), UpdateBookingSettingsInput{DurationMin: 60}); !errors.Is(err, ErrBookingNotOwner) {
		t.Errorf("staff update: got %v, want ErrBookingNotOwner", err)
	}

	bad := []UpdateBookingSettingsInput{
		{DurationMin: 0}, {DurationMin: 10}, {DurationMin: 481},
		{DurationMin: 60, BufBefore: -1}, {DurationMin: 60, BufAfter: 121},
		{DurationMin: 60, MinNoticeHours: 721}, {DurationMin: 60, MaxAdvanceDays: 0},
		{DurationMin: 60, MaxAdvanceDays: 366},
		{DurationMin: 60, CancelPolicy: strings.Repeat("x", 2001)},
	}
	for i, in := range bad {
		if _, err := svc.UpdateSettings(ctx, owner, in); !errors.Is(err, ErrBookingSettingsBad) {
			t.Errorf("case %d: got %v, want ErrBookingSettingsBad", i, err)
		}
	}

	got, err := svc.UpdateSettings(ctx, owner, UpdateBookingSettingsInput{
		Accepting: true, DurationMin: 45, BufBefore: 10, BufAfter: 15,
		MinNoticeHours: 24, MaxAdvanceDays: 60, CancelPolicy: "  24h notice  ",
	})
	if err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	if got.DurationMin != 45 || got.CancelPolicy != "24h notice" || store.saved == nil {
		t.Errorf("not normalized/persisted: %+v", got)
	}
}

func TestBookingSetHours_Validation(t *testing.T) {
	svc := NewBookingService(BookingServiceDeps{Store: &stubBookingStore{}})
	ctx := context.Background()
	owner := bookingTestScope(tenant.RoleOwner)

	if err := svc.SetHours(ctx,
		bookingTestScope(tenant.RoleStaff), DefaultWeekHours()); !errors.Is(err, ErrBookingNotOwner) {
		t.Errorf("staff hours: got %v, want ErrBookingNotOwner", err)
	}
	// Six rows is not a week.
	if err := svc.SetHours(ctx, owner, DefaultWeekHours()[:6]); !errors.Is(err, ErrBookingSettingsBad) {
		t.Errorf("short week: got %v, want ErrBookingSettingsBad", err)
	}
	// Open day ending before it starts.
	bad := DefaultWeekHours()
	bad[1] = BookingHour{Dow: 1, OpenMin: 1020, CloseMin: 540, IsOpen: true}
	if err := svc.SetHours(ctx, owner, bad); !errors.Is(err, ErrBookingSettingsBad) {
		t.Errorf("inverted day: got %v, want ErrBookingSettingsBad", err)
	}
	if err := svc.SetHours(ctx, owner, DefaultWeekHours()); err != nil {
		t.Errorf("valid week rejected: %v", err)
	}
}

func TestBookingReschedule_Validation(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Lagos")
	// Next Monday 10:00-11:00 Lagos (always future, notice satisfied).
	now := time.Now().In(loc)
	days := (8 - int(now.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	day := now.AddDate(0, 0, days)
	y, m, d := day.Date()
	start := time.Date(y, m, d, 10, 0, 0, 0, loc)
	store := &stubBookingStore{
		booking: &Booking{
			ID: orgIDForMove(), Status: "REQUESTED",
			StartAt: start, EndAt: start.Add(time.Hour), Timezone: "Africa/Lagos",
		},
	}
	svc := NewBookingService(BookingServiceDeps{Store: store})
	ctx := context.Background()
	owner := bookingTestScope(tenant.RoleOwner)
	id := store.booking.ID

	at := func(day time.Time, hh, mm int) (time.Time, time.Time) {
		yy, mo, dd := day.Date()
		s := time.Date(yy, mo, dd, hh, mm, 0, 0, loc)
		return s, s.Add(time.Hour)
	}
	mustBadSlot := func(label string, err error) {
		t.Helper()
		if !errors.Is(err, ErrBookingBadSlot) {
			t.Fatalf("%s: got %v, want ErrBookingBadSlot", label, err)
		}
		if store.moved {
			t.Fatalf("%s: rejected move must not reach the store", label)
		}
	}

	// Sunday is closed by default.
	// Next Sunday 10:00 (closed by default): +6 from Monday.
	sun := day.AddDate(0, 0, (7-int(day.Weekday()))%7)
	if sun.Equal(day) {
		sun = sun.AddDate(0, 0, 7)
	}
	ss, se := at(sun, 10, 0)
	mustBadSlot("sunday", func() error {
		_, err := svc.RescheduleBooking(ctx, owner, id, ss, se)
		return err
	}())

	// Past move.
	past := time.Now().Add(-48 * time.Hour)
	mustBadSlot("past", func() error {
		_, err := svc.RescheduleBooking(ctx, owner, id, past, past.Add(time.Hour))
		return err
	}())

	// Changed duration.
	s13, _ := at(day, 13, 0)
	mustBadSlot("duration", func() error {
		_, err := svc.RescheduleBooking(ctx, owner, id, s13, s13.Add(90*time.Minute))
		return err
	}())

	// Valid move reaches the store.
	if _, err := svc.RescheduleBooking(ctx, owner, id, s13, s13.Add(time.Hour)); err != nil {
		t.Fatalf("valid move: %v", err)
	}
	if !store.moved {
		t.Fatal("valid move must reach the store")
	}
}

func orgIDForMove() uuid.UUID { return uuid.New() }
