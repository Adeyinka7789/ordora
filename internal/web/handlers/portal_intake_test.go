package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// intakeTestProducts builds a catalog with one question each.
func intakeTestProducts() []intakeProductView {
	p1, p2 := uuid.NewString(), uuid.NewString()
	return []intakeProductView{
		{ID: p1, Name: "Senator", Questions: []intakeQuestionView{
			{Key: "color", Label: "Color?", Required: true},
			{Key: "notes", Label: "Notes?"},
		}},
		{ID: p2, Name: "Kaftan"},
	}
}

func intakePostForm(t *testing.T, pid, qkey, answer string) *http.Request {
	t.Helper()
	form := url.Values{}
	form.Set("pq_"+pid+"_"+qkey, answer)
	req := httptest.NewRequest("POST", "/order/x", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	return req
}

// TestParseIntakeAnswers_ResolvesLabelsAndFilters verifies answer parsing:
// labels resolve from the catalog, unknown products/keys drop, blanks drop,
// and picked-only filtering works.
func TestParseIntakeAnswers_ResolvesLabelsAndFilters(t *testing.T) {
	products := intakeTestProducts()
	p1 := products[0].ID

	form := url.Values{}
	form.Set("pq_"+p1+"_color", "Navy")
	form.Set("pq_"+p1+"_notes", "  ") // blank dropped
	form.Set("pq_"+uuid.NewString()+"_color", "X") // unknown product dropped
	form.Set("pq_"+p1+"_nope", "Y")               // unknown key kept with raw key
	req := httptest.NewRequest("POST", "/order/x", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}

	got, _ := parseIntakeAnswers(req, products, nil)
	if len(got) != 2 {
		t.Fatalf("got %d answers, want 2 (color + raw key)", len(got))
	}
	found := map[string]string{}
	for _, a := range got {
		found[a.Q] = a.A
	}
	if found["Color?"] != "Navy" {
		t.Errorf("label not resolved: %v", found)
	}

	// Picked-only: answers for unpicked products are dropped.
	picked := map[string]struct{}{products[1].ID: {}}
	got, _ = parseIntakeAnswers(req, products, picked)
	if len(got) != 0 {
		t.Errorf("unpicked answers must drop, got %v", got)
	}
}

// TestMissingRequiredAnswer enforces required product questions.
func TestMissingRequiredAnswer(t *testing.T) {
	products := intakeTestProducts()
	picked := map[string]struct{}{products[0].ID: {}}

	req := intakePostForm(t, products[0].ID, "notes", "hi")
	answered := answeredKeys(req, products, picked)
	if msg := missingRequiredAnswer(products, picked, answered); msg == "" {
		t.Error("missing required color answer must produce a message")
	}

	req = intakePostForm(t, products[0].ID, "color", "Navy")
	answered = answeredKeys(req, products, picked)
	if msg := missingRequiredAnswer(products, picked, answered); msg != "" {
		t.Errorf("answered required question must pass, got %q", msg)
	}

	// Unpicked products are never gated.
	answered = map[string]struct{}{}
	if msg := missingRequiredAnswer(products, map[string]struct{}{}, answered); msg != "" {
		t.Errorf("nothing picked must pass, got %q", msg)
	}
}
