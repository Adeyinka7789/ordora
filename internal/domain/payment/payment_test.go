package payment

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

func m(minor int64) money.Money { return money.MustNew(minor, "NGN") }

func TestNew_Valid(t *testing.T) {
	p, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(50_000), MethodBankTransfer, "GTB-1234",
		time.Now(), "deposit", uuid.New(), time.Now(),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Amount.Amount() != 50_000 {
		t.Fatalf("amount = %d", p.Amount.Amount())
	}
	if p.IsReversal() || p.IsReversed() {
		t.Fatal("new payment should be normal")
	}
}

func TestNew_AmountZero(t *testing.T) {
	_, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(0), MethodCash, "", time.Now(), "", uuid.New(), time.Now(),
	)
	if !errors.Is(err, ErrAmountZero) {
		t.Fatalf("expected ErrAmountZero, got %v", err)
	}
}

func TestNew_AmountNegative(t *testing.T) {
	_, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(-1000), MethodCash, "", time.Now(), "", uuid.New(), time.Now(),
	)
	if !errors.Is(err, ErrAmountNegative) {
		t.Fatalf("expected ErrAmountNegative, got %v", err)
	}
}

func TestNew_MethodInvalid(t *testing.T) {
	_, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(1000), Method("BITCOIN"), "", time.Now(), "", uuid.New(), time.Now(),
	)
	if !errors.Is(err, ErrMethodInvalid) {
		t.Fatalf("expected ErrMethodInvalid, got %v", err)
	}
}

func TestNew_PaidAtRequired(t *testing.T) {
	_, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(1000), MethodCash, "", time.Time{}, "", uuid.New(), time.Now(),
	)
	if !errors.Is(err, ErrPaidAtMissing) {
		t.Fatalf("expected ErrPaidAtMissing, got %v", err)
	}
}

func TestNew_ReferenceTooLong(t *testing.T) {
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	_, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		m(1000), MethodCash, string(long), time.Now(), "", uuid.New(), time.Now(),
	)
	if !errors.Is(err, ErrReferenceTooLong) {
		t.Fatalf("expected ErrReferenceTooLong, got %v", err)
	}
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		total, paid int64
		want        Status
	}{
		{100_000, 0, StatusUnpaid},
		{100_000, 50_000, StatusPartiallyPaid},
		{100_000, 99_999, StatusPartiallyPaid},
		{100_000, 100_000, StatusPaid},
		{100_000, 150_000, StatusPaid}, // overpayment still counts as paid
	}
	for _, c := range cases {
		got := DeriveStatus(m(c.total), m(c.paid))
		if got != c.want {
			t.Errorf("total=%d paid=%d: got %s, want %s", c.total, c.paid, got, c.want)
		}
	}
}

func TestMethod_IsValid(t *testing.T) {
	valid := []Method{MethodCash, MethodBankTransfer, MethodCard, MethodPOS, MethodOther}
	for _, m := range valid {
		if !m.IsValid() {
			t.Errorf("%s should be valid", m)
		}
	}
	if Method("").IsValid() {
		t.Error("empty method should be invalid")
	}
	if Method("PAYPAL").IsValid() {
		t.Error("PAYPAL should be invalid (not implemented)")
	}
}
