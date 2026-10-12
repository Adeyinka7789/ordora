package handlers

import (
	"strings"
	"testing"

	"github.com/Adeyinka7789/ordora/internal/domain/attachment"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

// TestCountProductImages pins the sample-photo cap rule: cover + gallery
// count, everything else (general files, proofs, inspiration) does not.
func TestCountProductImages(t *testing.T) {
	list := []*attachment.Attachment{
		{Purpose: attachment.PurposeProductCover},
		{Purpose: attachment.PurposeProductGallery},
		{Purpose: attachment.PurposeProductGallery},
		{Purpose: attachment.PurposeGeneral},
		{Purpose: attachment.PurposeInspiration},
		nil,
	}
	if got := countProductImages(list); got != 3 {
		t.Errorf("count = %d, want 3 (cover + 2 gallery)", got)
	}
	if got := countProductImages(nil); got != 0 {
		t.Errorf("nil list count = %d, want 0", got)
	}
}

// TestHumanizeTooManyImages keeps the cap rejection on a plain message,
// not a raw sentinel.
func TestHumanizeTooManyImages(t *testing.T) {
	msg := humanizeAttachmentError(attachment.ErrTooManyImages)
	if !strings.Contains(msg, "at most 5 sample photos") {
		t.Errorf("cap message must state the limit, got %q", msg)
	}
}

// TestIntakeCapsGalleryAtFive guards the display side of the cap: a
// legacy product with 7 images only exposes the first 5 publicly.
func TestIntakeCapsGalleryAtFive(t *testing.T) {
	r, err := render.New("../templates")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
		"66666666-6666-4666-8666-666666666666",
		"77777777-7777-4777-8777-777777777777",
	}
	out, err := r.Raw("portal/intake.html", intakePageData{
		Title: "Place an order", OrgName: "Ada Shots", Slug: "ada-shots",
		Products: []intakeProductView{
			{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Wedding Premium",
				Category: "Weddings", PriceMinor: 500000, Currency: "NGN",
				Availability: "in_stock", ImageIDs: ids,
				CoverImageID: ids[0]},
		},
		Categories: []string{"Weddings"},
		Quantities: map[string]string{}, Answers: map[string]string{},
		Vocab: org.VocabForCategory(org.PhotographyCategory),
	})
	if err != nil {
		t.Fatalf("portal/intake.html (cap): %v", err)
	}
	want := `data-images="` + strings.Join(ids[:5], ",") + `"`
	if !strings.Contains(out, want) {
		t.Errorf("gallery must expose exactly the first 5 images, want %q", want)
	}
	if strings.Contains(out, ids[5]) || strings.Contains(out, ids[6]) {
		t.Error("images 6-7 must not reach the public page")
	}
}
