package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. These are the OWASP 2024+ baseline for interactive
// logins (64 MiB memory, 3 iterations, 2 threads). Tune upward only if you
// can afford it in your VPS RAM budget; going below these weakens the hash
// meaningfully.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024 // KiB → 64 MiB
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

var (
	ErrPasswordTooShort = errors.New("auth: password must be at least 10 characters")
	ErrPasswordTooLong  = errors.New("auth: password must be at most 256 characters")
	ErrInvalidHash      = errors.New("auth: invalid password hash format")
	ErrIncompatibleHash = errors.New("auth: incompatible password hash version")
)

// ValidatePasswordStrength applies our minimum policy. Enforced at registration
// and password reset.
func ValidatePasswordStrength(pw string) error {
	if len(pw) < 10 {
		return ErrPasswordTooShort
	}
	if len(pw) > 256 {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns a PHC-formatted Argon2id string:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<base64 salt>$<base64 hash>
//
// This is portable and self-describing, so we can raise parameters later
// without breaking existing users.
func HashPassword(password string) (string, error) {
	if err := ValidatePasswordStrength(password); err != nil {
		return "", err
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: rand: %w", err)
	}

	key := argon2.IDKey(
		[]byte(password),
		salt,
		argonTime,
		argonMemory,
		argonThreads,
		argonKeyLen,
	)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
	return encoded, nil
}

// VerifyPassword checks a plaintext password against a PHC-encoded Argon2id
// hash. Returns nil on match, ErrInvalidHash on mismatch or malformed hash.
//
// Uses subtle.ConstantTimeCompare to avoid timing leaks on the key comparison.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrInvalidHash
	}
	if version != argon2.Version {
		return ErrIncompatibleHash
	}

	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrInvalidHash
	}
	wantKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrInvalidHash
	}

	gotKey := argon2.IDKey(
		[]byte(password),
		salt,
		timeCost,
		memory,
		threads,
		uint32(len(wantKey)),
	)

	if subtle.ConstantTimeCompare(gotKey, wantKey) != 1 {
		return ErrInvalidHash
	}
	return nil
}
