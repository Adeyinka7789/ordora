package auth

import (
	"testing"

	"github.com/google/uuid"
)

// TestCurrentSessionID pins the impersonation contract: synthetic
// sessions carry no business session, so callers get ok=false instead
// of a nil dereference (which panicked GET /profile into a 500).
func TestCurrentSessionID(t *testing.T) {
	id := uuid.New()
	s := &ResolvedSession{Session: &Session{ID: id}}
	if got, ok := s.CurrentSessionID(); !ok || got != id {
		t.Errorf("real session: got %v, %v; want %v, true", got, ok, id)
	}

	synth := &ResolvedSession{}
	if _, ok := synth.CurrentSessionID(); ok {
		t.Error("synthetic session without Session must report ok=false")
	}

	var nilSession *ResolvedSession
	if _, ok := nilSession.CurrentSessionID(); ok {
		t.Error("nil session must report ok=false")
	}
}
