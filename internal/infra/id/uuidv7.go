package id

import (
	"crypto/rand"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
)

// New returns a UUIDv7.
//
// UUIDv7 = 48-bit big-endian Unix milliseconds + 74 random bits.
// Properties we care about:
//   - Time-sortable, so B-tree indexes stay dense on insert.
//   - Non-sequential enough to not leak row counts (unlike SERIAL).
//   - Compatible with Postgres UUID type, no extension needed.
//
// We use crypto/rand, not math/rand. UUIDs may appear in URLs (public tokens
// are a different value entirely, but ids leak through the portal indirectly),
// and predictable ids are a footgun.
func New() uuid.UUID {
	var b [16]byte

	// 48-bit millisecond timestamp, big-endian, shifted left by 16 bits.
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint64(b[0:8], ms<<16)

	// Fill everything with randomness.
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read only fails if the OS RNG is broken.
		// That's not a recoverable condition for a web server.
		panic("id: crypto/rand failed: " + err.Error())
	}

	// Re-apply the timestamp (rand overwrote it).
	binary.BigEndian.PutUint64(b[0:8], ms<<16)

	// Set version (7) and variant (RFC 4122) bits.
	b[6] = (b[6] & 0x0F) | 0x70
	b[8] = (b[8] & 0x3F) | 0x80

	return uuid.UUID(b)
}

// MustParse is a convenience for tests and constants.
func MustParse(s string) uuid.UUID {
	return uuid.MustParse(s)
}
