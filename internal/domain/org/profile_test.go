package org

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func testOrg(t *testing.T) *Organization {
	t.Helper()
	o, err := New(uuid.New(), "Test Biz", Slug{value: "test-biz"}, time.Now())
	if err != nil {
		t.Fatalf("new org: %v", err)
	}
	return o
}

func TestSetProfileValid(t *testing.T) {
	o := testOrg(t)
	err := o.SetProfile("Retail", "Tailoring & Fashion", "2–5", "WhatsApp", "+2348012345678", "12 Allen Ave", time.Now())
	if err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	if o.BusinessCategory != "Tailoring & Fashion" || o.Phone != "+2348012345678" {
		t.Errorf("profile not stored: %+v", o)
	}
}

func TestSetProfileCustomOtherText(t *testing.T) {
	o := testOrg(t)
	err := o.SetProfile("Cooperative", "Shoe making", "", "", "+2348012345678", "", time.Now())
	if err != nil {
		t.Fatalf("custom Other text rejected: %v", err)
	}
	if o.BusinessCategory != "Shoe making" || o.BusinessType != "Cooperative" {
		t.Errorf("custom text not stored: %+v", o)
	}
}

func TestSetProfileRejects(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name                             string
		typ, cat, size, ref, phone, addr string
		want                             error
	}{
		{"missing category", "", "", "", "", "+2341", "", ErrProfileCategoryRequired},
		{"raw Other sentinel", "", "Other", "", "", "+2341", "", ErrProfileCategoryInvalid},
		{"overlong custom", "", string(make([]byte, 121)), "", "", "+2341", "", ErrProfileCategoryInvalid},
		{"raw Other type", "Other", "Retail", "", "", "+2341", "", ErrProfileTypeInvalid},
		{"bad team size", "", "Retail", "1000", "", "+2341", "", ErrProfileTeamSizeInvalid},
		{"raw Other referral", "", "Retail", "", "Other", "+2341", "", ErrProfileReferralInvalid},
		{"missing phone", "", "Retail", "", "", "", "", ErrProfilePhoneInvalid},
		{"long phone", "", "Retail", "", "", string(make([]byte, 41)), "", ErrProfilePhoneInvalid},
	}
	for _, tc := range cases {
		o := testOrg(t)
		if err := o.SetProfile(tc.typ, tc.cat, tc.size, tc.ref, tc.phone, tc.addr, now); err != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestResolveOther(t *testing.T) {
	if got := ResolveOther("Retail", ""); got != "Retail" {
		t.Errorf("plain value: got %q", got)
	}
	if got := ResolveOther("Other", "  Shoe making  "); got != "Shoe making" {
		t.Errorf("other resolves custom: got %q", got)
	}
	if got := ResolveOther("Other", ""); got != "" {
		t.Errorf("empty custom: got %q", got)
	}
}
