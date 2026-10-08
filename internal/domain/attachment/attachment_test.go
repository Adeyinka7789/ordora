package attachment

import (
	"testing"
)

func TestNormalizePurpose(t *testing.T) {
	for in, want := range map[string]string{
		"general":         PurposeGeneral,
		"inspiration":     PurposeInspiration,
		"payment_proof":   PurposePaymentProof,
		"PAYMENT_PROOF":   PurposePaymentProof,
		"product_gallery": PurposeProductGallery,
		"product_cover":   PurposeProductCover,
		"  cover  ":       PurposeGeneral, // unknown stays general
		"":                PurposeGeneral,
	} {
		if got := NormalizePurpose(in); got != want {
			t.Errorf("NormalizePurpose(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomerVisible(t *testing.T) {
	visible := []string{PurposeGeneral, PurposeInspiration, PurposePaymentProof}
	for _, p := range visible {
		if !(&Attachment{Purpose: p}).IsCustomerVisible() {
			t.Errorf("purpose %q must be customer-visible", p)
		}
	}
	hidden := []string{PurposeProductGallery, PurposeProductCover, "internal"}
	for _, p := range hidden {
		if (&Attachment{Purpose: p}).IsCustomerVisible() {
			t.Errorf("purpose %q must NOT be customer-visible", p)
		}
	}
}

func TestEntityProductValid(t *testing.T) {
	if !EntityProduct.Valid() {
		t.Error("EntityProduct must be valid")
	}
	if EntityType("ORDERX").Valid() {
		t.Error("unknown entity must be invalid")
	}
}
