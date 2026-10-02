// Package email provides transactional email delivery.
//
// Two implementations ship with Ordora:
//
//   - ConsoleMailer — logs emails to stdout (development)
//   - SMTPMailer    — sends via any SMTP server (production)
//
// Both implement Mailer. The rest of the application never knows which
// one is in use.
package email

import (
	"context"
	"errors"
)

// Message is one transactional email.
type Message struct {
	To      string // one recipient
	Subject string
	HTML    string // rendered HTML body
	Text    string // rendered plain-text body (fallback)
}

// Mailer sends transactional emails.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Errors.
var (
	ErrNoRecipient = errors.New("email: recipient is required")
	ErrNoSubject   = errors.New("email: subject is required")
	ErrNoBody      = errors.New("email: body is required")
)

// validate applies common checks before sending.
func validate(msg Message) error {
	if msg.To == "" {
		return ErrNoRecipient
	}
	if msg.Subject == "" {
		return ErrNoSubject
	}
	if msg.HTML == "" && msg.Text == "" {
		return ErrNoBody
	}
	return nil
}
