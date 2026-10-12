package app

import (
	"testing"

	"github.com/Adeyinka7789/ordora/internal/domain/org"
)

// TestPublicOrgVocab pins the slug-loading -> vocabulary wiring: the
// category that LookupOrgBySlug returns decides the storefront words.
// Blank (older orgs) reads as the generic product storefront.
func TestPublicOrgVocab(t *testing.T) {
	photo := &PublicOrg{BusinessCategory: org.PhotographyCategory}
	if v := photo.Vocab(); v.Mode != org.StorefrontPhotography || v.Item != "Package" || v.Group != "Event type" {
		t.Errorf("photo org vocab: %+v", v)
	}

	blank := &PublicOrg{}
	if v := blank.Vocab(); v.Item != "Product" || v.Group != "Category" {
		t.Errorf("blank category must read as generic: %+v", v)
	}

	var nilOrg *PublicOrg
	if v := nilOrg.Vocab(); v.Item != "Product" {
		t.Errorf("nil org must read as generic: %+v", v)
	}
}
