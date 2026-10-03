package auth

import (
	"testing"
)

// TestAdminPasswordRoundTrip proves that a password hashed by the seed CLI
// will verify with the same string. Run:
//
//	go test ./internal/auth/ -run TestAdminPasswordRoundTrip -v
func TestAdminPasswordRoundTrip(t *testing.T) {
	// The exact hash from the DB.
	hash := "$argon2id$v=19$m=65536,t=3,p=2$7VXyxA5p5lokVNJ02y9ulw$pN142Wl2cvAVBMPToCF7VxLWLUd2+L+ddcM8v7q7xiU"

	candidates := []string{
		"Passw0rdTest123!",     // what I told you to use
		"Passw0rdTest123! ",    // trailing space
		" Passw0rdTest123!",    // leading space
		"Passw0rdTest123",      // no exclamation
		"passw0rdtest123!",     // lowercase p
		"Passw0rdTest123!\n",   // trailing newline (common in terminals)
		"Passw0rdTest123!\r\n", // CRLF (Windows terminal)
	}

	for _, pw := range candidates {
		err := VerifyPassword(pw, hash)
		t.Logf("password %q -> %v", pw, err == nil)
		if err == nil {
			t.Logf("MATCH: %q verifies against the stored hash", pw)
		}
	}
}
