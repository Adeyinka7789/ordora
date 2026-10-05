package render

import (
	"testing"
	"text/template"
)

func TestTemplateFuncs_HasDict(t *testing.T) {
	fm := templateFuncs()
	if _, ok := fm["dict"]; !ok {
		t.Fatal("dict is not in templateFuncs()")
	}
	if _, ok := fm["add1"]; !ok {
		t.Fatal("add1 is not in templateFuncs()")
	}
	if _, ok := fm["list"]; !ok {
		t.Fatal("list is not in templateFuncs()")
	}
	if _, ok := fm["contact"]; !ok {
		t.Fatal("contact is not in templateFuncs()")
	}
}

func TestTemplateFuncs_ParsesDictUsage(t *testing.T) {
	tmpl := template.New("").Funcs(templateFuncs())
	_, err := tmpl.New("test").Parse(`{{ $x := dict "a" 1 "b" 2 }}{{ index $x "a" }}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
}

func TestCurrency_Grouping(t *testing.T) {
	fm := templateFuncs()
	currency, ok := fm["currency"].(func(int64, string) string)
	if !ok {
		t.Fatal("currency func has unexpected signature")
	}
	cases := map[int64]string{
		0:           "NGN 0.00",
		5:           "NGN 0.05",
		99:          "NGN 0.99",
		100:         "NGN 1.00",
		99999:       "NGN 999.99",
		100000:      "NGN 1,000.00",
		19350000:    "NGN 193,500.00",
		123456789:   "NGN 1,234,567.89",
		12345678901: "NGN 123,456,789.01",
		-150:        "NGN -1.50",
		-123456789:  "NGN -1,234,567.89",
	}
	for minor, want := range cases {
		if got := currency(minor, "NGN"); got != want {
			t.Errorf("currency(%d) = %q, want %q", minor, got, want)
		}
	}
	if got := currency(100000, "USD"); got != "USD 1,000.00" {
		t.Errorf("currency code passthrough: got %q", got)
	}
}

// TestSub_MixedIntKinds locks the admin/ops.html fix: sub must accept int64
// money fields as well as plain ints (reports/index.html passes ints).
func TestSub_MixedIntKinds(t *testing.T) {
	fm := templateFuncs()
	sub, ok := fm["sub"].(func(any, any) int64)
	if !ok {
		t.Fatal("sub func has unexpected signature")
	}
	cases := []struct {
		a, b any
		want int64
	}{
		{50000, 20000, 30000},               // int, int (reports)
		{int64(50000), int64(20000), 30000}, // int64, int64 (admin ops)
		{int64(50000), 20000, 30000},        // mixed
		{100, int64(30), 70},                // untyped-ish constant with int64
		{0, 0, 0},
		{100, 150, -50},
	}
	for _, tc := range cases {
		if got := sub(tc.a, tc.b); got != tc.want {
			t.Errorf("sub(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
