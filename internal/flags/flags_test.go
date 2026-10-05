package flags

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func testProvider() *Provider {
	p := NewProvider(nil, 0)
	p.flags = map[string]Flag{
		"on":      {Key: "on", Enabled: true, RolloutPercent: 100},
		"off":     {Key: "off", Enabled: false, RolloutPercent: 100},
		"zero":    {Key: "zero", Enabled: true, RolloutPercent: 0},
		"half":    {Key: "half", Enabled: true, RolloutPercent: 50},
		"offhalf": {Key: "offhalf", Enabled: false, RolloutPercent: 50},
	}
	return p
}

func TestEnabled_Basics(t *testing.T) {
	p := testProvider()
	org := uuid.New()

	if !p.Enabled(org, "on") {
		t.Error("enabled@100% should be on")
	}
	if p.Enabled(org, "off") {
		t.Error("disabled flag should be off")
	}
	if p.Enabled(org, "zero") {
		t.Error("0% rollout should be off")
	}
	if p.Enabled(org, "offhalf") {
		t.Error("disabled flag should be off regardless of percent")
	}
	if p.Enabled(org, "nope-missing") {
		t.Error("unknown flag should be off (fail-closed)")
	}
}

func TestEnabled_RolloutDeterministicAndSplit(t *testing.T) {
	p := testProvider()
	org := uuid.New()

	first := p.Enabled(org, "half")
	for i := 0; i < 50; i++ {
		if p.Enabled(org, "half") != first {
			t.Fatal("rollout must be deterministic per org")
		}
	}

	// Across many orgs, ~50% should be on (generous bounds).
	on := 0
	const n = 2000
	for i := 0; i < n; i++ {
		if p.Enabled(uuid.New(), "half") {
			on++
		}
	}
	if on < n*30/100 || on > n*70/100 {
		t.Errorf("50%% rollout gave %d/%d on", on, n)
	}
}

func TestRefresh_LoadsSnapshot(t *testing.T) {
	p := NewProvider(func(ctx context.Context) ([]Flag, error) {
		return []Flag{{Key: "fresh", Enabled: true, RolloutPercent: 100}}, nil
	}, 0)
	if p.Enabled(uuid.New(), "fresh") {
		t.Fatal("should be off before first refresh")
	}
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !p.Enabled(uuid.New(), "fresh") {
		t.Error("should be on after refresh")
	}
	if got := len(p.All()); got != 1 {
		t.Errorf("All() = %d flags, want 1", got)
	}
}

func TestEnabled_OverrideWins(t *testing.T) {
	org := uuid.New()
	other := uuid.New()
	p := NewProvider(func(ctx context.Context) ([]Flag, error) {
		return []Flag{
			{Key: "on", Enabled: true, RolloutPercent: 100},
			{Key: "off", Enabled: false, RolloutPercent: 100},
		}, nil
	}, 0).WithOverrideLoader(func(ctx context.Context) ([]Override, error) {
		return []Override{
			{FlagKey: "on", OrgID: org, Enabled: false},
			{FlagKey: "off", OrgID: org, Enabled: true},
		}, nil
	})
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if p.Enabled(org, "on") {
		t.Error("override off must beat global on")
	}
	if !p.Enabled(org, "off") {
		t.Error("override on must beat global off")
	}
	if !p.Enabled(other, "on") || p.Enabled(other, "off") {
		t.Error("orgs without overrides must follow the global rule")
	}
	if en, ok := p.Overridden(org, "on"); !ok || en {
		t.Error("Overridden should report (false, true)")
	}
	if _, ok := p.Overridden(other, "on"); ok {
		t.Error("Overridden should report no override for other orgs")
	}
}
