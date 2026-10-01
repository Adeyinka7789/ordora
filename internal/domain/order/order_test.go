package order

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Adeyinka7789/ordora/internal/domain/money"
)

// ---- Helpers ----

func mustMoney(t *testing.T, minor int64, cur string) money.Money {
	t.Helper()
	m, err := money.New(minor, cur)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func makeOrder(t *testing.T) *Order {
	t.Helper()
	o, err := New(
		uuid.New(), uuid.New(), uuid.New(),
		"ORD-2026-000001", "Test order", "", "NGN",
		uuid.New(), time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func makeItem(t *testing.T, desc string, qty int64, unitMinor int64) *Item {
	t.Helper()
	it, err := NewItem(uuid.New(), desc, qty, mustMoney(t, unitMinor, "NGN"), 0)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// ---- Status tests ----

func TestStatus_CanTransitionTo(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusNew, StatusConfirmed, true},
		{StatusNew, StatusInProgress, false},
		{StatusNew, StatusCancelled, true},
		{StatusConfirmed, StatusInProgress, true},
		{StatusInProgress, StatusReady, true},
		{StatusReady, StatusDelivered, true},
		{StatusReady, StatusOutForDelivery, true},
		{StatusOutForDelivery, StatusDelivered, true},
		{StatusDelivered, StatusCompleted, true},
		{StatusCompleted, StatusCancelled, false},
		{StatusCancelled, StatusNew, false},
		{StatusCompleted, StatusCompleted, false},
	}
	for _, c := range cases {
		if got := c.from.CanTransitionTo(c.to); got != c.want {
			t.Errorf("%s -> %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestStatus_Terminal(t *testing.T) {
	if !StatusCompleted.IsTerminal() {
		t.Error("COMPLETED should be terminal")
	}
	if !StatusCancelled.IsTerminal() {
		t.Error("CANCELLED should be terminal")
	}
	if StatusInProgress.IsTerminal() {
		t.Error("IN_PROGRESS should not be terminal")
	}
}

// ---- Order tests ----

func TestOrder_RequiresItems(t *testing.T) {
	o := makeOrder(t)
	if err := o.Validate(); !errors.Is(err, ErrNoItems) {
		t.Fatalf("expected ErrNoItems, got %v", err)
	}
}

func TestOrder_AddItem_ComputesTotals(t *testing.T) {
	o := makeOrder(t)

	// 2 items at ₦500.00 each = ₦1000.00
	it := makeItem(t, "Widget", 2*QuantityScale, 50_000)
	if err := o.AddItem(it); err != nil {
		t.Fatal(err)
	}
	if o.Subtotal.Amount() != 100_000 {
		t.Fatalf("subtotal = %d, want 100000", o.Subtotal.Amount())
	}
	if o.Total.Amount() != 100_000 {
		t.Fatalf("total = %d, want 100000", o.Total.Amount())
	}

	// Add a third at ₦250.00 → total ₦1250.00
	it2 := makeItem(t, "Gadget", 1*QuantityScale, 25_000)
	if err := o.AddItem(it2); err != nil {
		t.Fatal(err)
	}
	if o.Total.Amount() != 125_000 {
		t.Fatalf("total = %d, want 125000", o.Total.Amount())
	}
}

func TestOrder_FractionalQuantity(t *testing.T) {
	o := makeOrder(t)
	// 1.5 kg at ₦200.00/kg = ₦300.00
	it, err := NewItem(uuid.New(), "Coffee beans", 1500, mustMoney(t, 20_000, "NGN"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if it.Subtotal.Amount() != 30_000 {
		t.Fatalf("subtotal = %d, want 30000", it.Subtotal.Amount())
	}
	if err := o.AddItem(it); err != nil {
		t.Fatal(err)
	}
	if o.Total.Amount() != 30_000 {
		t.Fatalf("total = %d, want 30000", o.Total.Amount())
	}
}

func TestOrder_DiscountAndTax(t *testing.T) {
	o := makeOrder(t)
	it := makeItem(t, "Widget", 1*QuantityScale, 100_000) // ₦1000.00
	_ = o.AddItem(it)

	_ = o.SetDiscount(mustMoney(t, 10_000, "NGN")) // ₦100.00
	_ = o.SetTax(mustMoney(t, 7_500, "NGN"))       // ₦75.00

	// Total = 1000 - 100 + 75 = 975
	if o.Total.Amount() != 97_500 {
		t.Fatalf("total = %d, want 97500", o.Total.Amount())
	}
}

func TestOrder_DiscountCannotExceedSubtotal(t *testing.T) {
	o := makeOrder(t)
	it := makeItem(t, "Widget", 1*QuantityScale, 10_000) // ₦100.00
	_ = o.AddItem(it)

	_ = o.SetDiscount(mustMoney(t, 20_000, "NGN")) // ₦200.00 > subtotal

	// Recomputation happened in SetDiscount. Check that o.Total reflects the
	// state after the invalid discount was applied. Since SetDiscount calls
	// recompute which returns ErrDiscountTooLarge and does NOT update Total
	// to a negative, but SetDiscount itself does not return that error, we
	// verify the invariant by calling Validate.
	//
	// Actually: SetDiscount should return the error. Let's assert that.
	if err := o.SetDiscount(mustMoney(t, 20_000, "NGN")); !errors.Is(err, ErrDiscountTooLarge) {
		t.Fatalf("expected ErrDiscountTooLarge, got %v", err)
	}
}

func TestOrder_ChangeStatus(t *testing.T) {
	o := makeOrder(t)
	_ = o.AddItem(makeItem(t, "Widget", 1*QuantityScale, 1000))

	now := time.Now()
	if err := o.ChangeStatus(StatusConfirmed, now); err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusConfirmed {
		t.Fatalf("status = %s", o.Status)
	}

	// Cannot skip forward.
	if err := o.ChangeStatus(StatusReady, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	// Legal sequence.
	_ = o.ChangeStatus(StatusInProgress, now)
	_ = o.ChangeStatus(StatusReady, now)
	_ = o.ChangeStatus(StatusDelivered, now)
	if o.DeliveredAt == nil {
		t.Fatal("DeliveredAt should be set")
	}
	_ = o.ChangeStatus(StatusCompleted, now)

	// Terminal.
	if err := o.ChangeStatus(StatusCancelled, now); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("expected ErrAlreadyCompleted, got %v", err)
	}
}

func TestOrder_CancelFromAnyNonTerminal(t *testing.T) {
	states := []Status{StatusNew, StatusConfirmed, StatusInProgress, StatusReady}
	for _, s := range states {
		o := makeOrder(t)
		_ = o.AddItem(makeItem(t, "Widget", 1*QuantityScale, 1000))
		o.Status = s
		if err := o.ChangeStatus(StatusCancelled, time.Now()); err != nil {
			t.Errorf("cancel from %s: %v", s, err)
		}
	}
}

func TestOrder_Balance(t *testing.T) {
	o := makeOrder(t)
	_ = o.AddItem(makeItem(t, "Widget", 1*QuantityScale, 100_000)) // ₦1000.00
	o.SetAmountPaid(mustMoney(t, 40_000, "NGN"))                   // ₦400.00

	if o.Balance().Amount() != 60_000 {
		t.Fatalf("balance = %d, want 60000", o.Balance().Amount())
	}
	if o.IsFullyPaid() {
		t.Fatal("should not be fully paid")
	}

	o.SetAmountPaid(mustMoney(t, 100_000, "NGN"))
	if !o.IsFullyPaid() {
		t.Fatal("should be fully paid")
	}
}

func TestOrderNumber(t *testing.T) {
	n := FormatOrderNumber(2026, 128)
	if n != "ORD-2026-000128" {
		t.Fatalf("number = %q", n)
	}
	if !IsValidOrderNumber(n) {
		t.Fatal("should be valid")
	}
	if IsValidOrderNumber("BAD-2026-001") {
		t.Fatal("should be invalid")
	}
	y, err := YearOf(n)
	if err != nil || y != 2026 {
		t.Fatalf("year = %d, err = %v", y, err)
	}
}
