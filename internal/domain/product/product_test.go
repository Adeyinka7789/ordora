package product

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

func m(minor int64) money.Money { return money.MustNew(minor, "NGN") }

func TestNew_Valid(t *testing.T) {
	p, err := New(
		uuid.New(), uuid.New(),
		"Senator Outfit", "Custom senator material", "SEN-001",
		m(45_000), uuid.New(), time.Now(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name != "Senator Outfit" {
		t.Fatalf("name = %q", p.Name)
	}
	if !p.Active {
		t.Fatal("new product should be active")
	}
	if p.UnitPrice.Amount() != 45_000 {
		t.Fatalf("price = %d", p.UnitPrice.Amount())
	}
}

func TestNew_NameRequired(t *testing.T) {
	_, err := New(uuid.New(), uuid.New(), "  ", "", "", m(100), uuid.New(), time.Now())
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestNew_NameTooLong(t *testing.T) {
	long := strings.Repeat("a", 201)
	_, err := New(uuid.New(), uuid.New(), long, "", "", m(100), uuid.New(), time.Now())
	if !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("expected ErrNameTooLong, got %v", err)
	}
}

func TestNew_NegativePrice(t *testing.T) {
	bad, _ := money.New(-100, "NGN")
	_, err := New(uuid.New(), uuid.New(), "X", "", "", bad, uuid.New(), time.Now())
	if !errors.Is(err, ErrPriceNegative) {
		t.Fatalf("expected ErrPriceNegative, got %v", err)
	}
}

func TestNew_CurrencyRequired(t *testing.T) {
	// A money value with no currency cannot be created via money.New,
	// so we can only test that the domain rejects a zero-value Money.
	_, err := New(uuid.New(), uuid.New(), "X", "", "", money.Money{}, uuid.New(), time.Now())
	if err == nil {
		t.Fatal("expected error for zero-value money")
	}
}

func TestUpdate(t *testing.T) {
	p, _ := New(uuid.New(), uuid.New(), "Old", "old desc", "OLD-1", m(1000), uuid.New(), time.Now())
	before := p.UpdatedAt

	time.Sleep(2 * time.Millisecond)

	err := p.Update("New", "new desc", "NEW-1", m(2000), time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p.Name != "New" || p.UnitPrice.Amount() != 2000 {
		t.Fatalf("update did not take: %+v", p)
	}
	if !p.UpdatedAt.After(before) {
		t.Fatal("UpdatedAt not advanced")
	}
}

func TestUpdate_CurrencyMismatch(t *testing.T) {
	p, _ := New(uuid.New(), uuid.New(), "X", "", "", m(1000), uuid.New(), time.Now())

	usd, _ := money.New(500, "USD")
	err := p.Update("X", "", "", usd, time.Now())
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestArchiveAndRestore(t *testing.T) {
	p, _ := New(uuid.New(), uuid.New(), "X", "", "", m(1000), uuid.New(), time.Now())

	p.Archive(time.Now())
	if p.Active {
		t.Fatal("archive should set Active = false")
	}
	p.Restore(time.Now())
	if !p.Active {
		t.Fatal("restore should set Active = true")
	}
}
