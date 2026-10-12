package handlers

import (
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestSettingsMeasurementCardGating pins the fashion-template boundary:
// tailoring orgs see the measurement-templates card, everyone else
// (photographers included) does not.
func TestSettingsMeasurementCardGating(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tailor, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormBizCat: "Tailoring & Fashion", IsTailoring: true,
	})
	if err != nil {
		t.Fatalf("settings/index.html (tailoring): %v", err)
	}
	if !strings.Contains(tailor, "Measurement templates") {
		t.Error("tailoring settings must show the measurement-templates card")
	}

	photo, err := r.Raw("settings/index.html", settingsPage{
		Title: "Settings", FormBizCat: "Photography & Media", IsTailoring: false,
	})
	if err != nil {
		t.Fatalf("settings/index.html (photography): %v", err)
	}
	if strings.Contains(photo, "Measurement templates") {
		t.Error("photography settings must not show fashion measurement templates")
	}
	if strings.Contains(photo, "ready-made garments") {
		t.Error("photography settings must not mention garments")
	}
}
