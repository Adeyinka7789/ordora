// Package audit defines the shape of audit log entries.
package audit

import "github.com/google/uuid"

// Entry is a single audit log record.
type Entry struct {
	OrganizationID uuid.UUID
	ActorUserID    uuid.UUID
	Action         string
	EntityType     string
	EntityID       uuid.UUID
	Before         []byte // JSON or nil
	After          []byte // JSON or nil
}
