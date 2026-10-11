package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
)

type stubEggStore struct {
	settings *PoultrySettings
	saved    *PoultrySettings
}

func (s *stubEggStore) GetSettings(_ context.Context, _ tenant.TenantScope) (*PoultrySettings, error) {
	if s.settings != nil {
		return s.settings, nil
	}
	return &PoultrySettings{MaxCratesPerDay: DefaultMaxCrates, AcceptingOrders: true}, nil
}

func (s *stubEggStore) UpsertSettings(_ context.Context, _ tenant.TenantScope, st *PoultrySettings) error {
	s.saved = st
	return nil
}

func (s *stubEggStore) SetAccepting(_ context.Context, _ tenant.TenantScope, _ bool, _ time.Time) error {
	return nil
}

func (s *stubEggStore) GetSold(_ context.Context, _ tenant.TenantScope, _ time.Time) (float64, error) {
	return 0, nil
}

func (s *stubEggStore) ReleaseCap(_ context.Context, _ tenant.TenantScope, _ time.Time, _ float64) error {
	return nil
}

func (s *stubEggStore) OrderCrates(_ context.Context, _ tenant.TenantScope, _ uuid.UUID) (float64, time.Time, error) {
	return 0, time.Time{}, nil
}

func (s *stubEggStore) OrderSummary(_ context.Context, _ tenant.TenantScope, _ uuid.UUID) (*EggOrderSummary, error) {
	return nil, ErrEggOrderNotFound
}

func (s *stubEggStore) ListPanelOrders(_ context.Context, _ tenant.TenantScope, _ time.Time) ([]EggPanelOrder, error) {
	return nil, nil
}

func eggTestScope(role tenant.Role) tenant.TenantScope {
	return tenant.TenantScope{OrgID: uuid.New(), UserID: uuid.New(), Role: role}
}

func TestEggUpdateSettings_Validation(t *testing.T) {
	store := &stubEggStore{}
	svc := NewEggService(EggServiceDeps{Store: store})
	ctx := context.Background()
	owner := eggTestScope(tenant.RoleOwner)

	// Staff may never touch settings (sales girl covers orders, not config).
	if _, err := svc.UpdateSettings(ctx,
		eggTestScope(tenant.RoleStaff), UpdateSettingsInput{MaxCratesPerDay: 50}); !errors.Is(err, ErrEggNotOwner) {
		t.Errorf("staff update: got %v, want ErrEggNotOwner", err)
	}

	for _, cap := range []int{0, -5, 100001} {
		if _, err := svc.UpdateSettings(ctx, owner,
			UpdateSettingsInput{MaxCratesPerDay: cap}); !errors.Is(err, ErrEggSettingsInvalid) {
			t.Errorf("cap %d: got %v, want ErrEggSettingsInvalid", cap, err)
		}
	}

	long := strings.Repeat("x", 200)
	if _, err := svc.UpdateSettings(ctx, owner, UpdateSettingsInput{
		MaxCratesPerDay: 50, BankName: long,
	}); !errors.Is(err, ErrEggSettingsInvalid) {
		t.Errorf("long bank name: got %v, want ErrEggSettingsInvalid", err)
	}

	got, err := svc.UpdateSettings(ctx, owner, UpdateSettingsInput{
		MaxCratesPerDay: 60, AcceptingOrders: true,
		BankName: "GTBank", AccountNumber: "0123456789",
		AccountName: "Egg Farm", Whatsapp: " 0803 ",
	})
	if err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	if got.MaxCratesPerDay != 60 || got.Whatsapp != "0803" {
		t.Errorf("not normalized/stored: %+v", got)
	}
	if store.saved == nil || store.saved.MaxCratesPerDay != 60 {
		t.Error("settings were not persisted")
	}
}

func TestDeliveryDay_Mapping(t *testing.T) {
	// Fixed UTC noon == 13:00 Lagos (WAT, no DST).
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	today, err := DeliveryDay("Africa/Lagos", "today", now)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if got := today.Format("2006-01-02"); got != "2026-10-12" {
		t.Errorf("today in Lagos: got %s", got)
	}
	tomorrow, err := DeliveryDay("Africa/Lagos", "tomorrow", now)
	if err != nil {
		t.Fatalf("tomorrow: %v", err)
	}
	if got := tomorrow.Format("2006-01-02"); got != "2026-10-13" {
		t.Errorf("tomorrow in Lagos: got %s", got)
	}
	if _, err := DeliveryDay("Africa/Lagos", "yesterday", now); err == nil {
		t.Error("unknown day must fail")
	}
	// Garbage timezone falls back to Lagos, not UTC (avoids day-boundary
	// surprises for Nigerian sellers).
	fallback, err := DeliveryDay("Not/AZone", "today", now)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if got := fallback.Format("2006-01-02"); got != "2026-10-12" {
		t.Errorf("fallback tz day: got %s", got)
	}
}

func TestFormatCrates(t *testing.T) {
	for in, want := range map[float64]string{
		67:  "67",
		0:   "0",
		2.5: "2.5",
	} {
		if got := FormatCrates(in); got != want {
			t.Errorf("FormatCrates(%v) = %q, want %q", in, got, want)
		}
	}
}
