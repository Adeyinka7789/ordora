package app

import (
	"testing"

	"github.com/google/uuid"
)

// TestSplitProofs guards the portal receipt's proof download links:
// ids and filenames stay aligned (same created_at order), malformed ids
// are dropped instead of breaking the row.
func TestSplitProofs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	proofs := splitProofs(
		a.String()+","+b.String(),
		"a.pdf, b.jpg",
	)
	if len(proofs) != 2 {
		t.Fatalf("got %d proofs, want 2", len(proofs))
	}
	if proofs[0].ID != a || proofs[0].Filename != "a.pdf" {
		t.Errorf("first proof misaligned: %+v", proofs[0])
	}
	if proofs[1].ID != b || proofs[1].Filename != "b.jpg" {
		t.Errorf("second proof misaligned: %+v", proofs[1])
	}

	if got := splitProofs("", ""); len(got) != 0 {
		t.Errorf("empty input must yield no proofs, got %v", got)
	}
	// Malformed id dropped, good one kept with its filename.
	proofs = splitProofs("nope,"+a.String(), "x.pdf, a.pdf")
	if len(proofs) != 1 || proofs[0].ID != a || proofs[0].Filename != "a.pdf" {
		t.Errorf("malformed id handling wrong: %+v", proofs)
	}
}

// TestNormalizePublicAnswers guards intake answer hygiene: blanks dropped,
// lengths capped, count capped.
func TestNormalizePublicAnswers(t *testing.T) {
	in := []PublicAnswer{
		{Product: "Senator", Q: "Color?", A: "  Navy  "},
		{Product: "X", Q: "Y", A: "   "}, // blank dropped
	}
	out := normalizePublicAnswers(in)
	if len(out) != 1 {
		t.Fatalf("got %d answers, want 1", len(out))
	}
	if out[0].A != "Navy" {
		t.Errorf("answer not trimmed: %q", out[0].A)
	}

	many := make([]PublicAnswer, 0, MaxPublicAnswers+5)
	for i := 0; i < MaxPublicAnswers+5; i++ {
		many = append(many, PublicAnswer{Product: "P", Q: "Q", A: "A"})
	}
	if got := normalizePublicAnswers(many); len(got) != MaxPublicAnswers {
		t.Errorf("answer cap = %d, want %d", len(got), MaxPublicAnswers)
	}
}
