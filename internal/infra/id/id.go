package id

import "github.com/google/uuid"

// Generator satisfies the auth.IDGen interface.
type Generator struct{}

// New returns a UUIDv7.
func (Generator) New() uuid.UUID {
	return New()
}
