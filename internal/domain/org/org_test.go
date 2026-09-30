package org

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSlug(t *testing.T) {
	cases := []struct {
		in      string
		wantErr error
	}{
		{"my-shop", nil},
		{"a1b", nil},
		{"My-Shop", nil}, // NewSlug lowercases input, so "My-Shop" → "my-shop" (valid)
		{"ab", ErrSlugLength},
		{"-bad", ErrSlugInvalid},
		{"bad-", ErrSlugInvalid},
		{"bad--slug", ErrSlugInvalid},
		{"has space", ErrSlugInvalid},
		{"has_underscore", ErrSlugInvalid},
		{"", ErrSlugRequired},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			_, err := NewSlug(c.in)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Big Shop":     "my-big-shop",
		"  A B  C  ":      "a-b-c",
		"ma--ny---dashes": "ma-ny-dashes",
		"trailing---":     "trailing",
		"---leading":      "leading",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewOrg(t *testing.T) {
	slug, _ := NewSlug("my-shop")
	o, err := New(uuid.New(), "My Shop", slug, time.Now())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if o.Currency != "NGN" || o.Timezone != "Africa/Lagos" {
		t.Fatalf("defaults wrong: %+v", o)
	}
}
