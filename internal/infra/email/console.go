package email

import (
	"context"
	"log/slog"
	"strings"
)

// ConsoleMailer logs emails to stdout instead of sending them.
// Development only. The output is visible in the server logs.
type ConsoleMailer struct{}

// NewConsoleMailer constructs a ConsoleMailer.
func NewConsoleMailer() *ConsoleMailer { return &ConsoleMailer{} }

// Send logs the message.
func (c *ConsoleMailer) Send(ctx context.Context, msg Message) error {
	if err := validate(msg); err != nil {
		return err
	}

	slog.Info("email (console)",
		"to", msg.To,
		"subject", msg.Subject,
	)
	// Emit the plain-text body as a separate multi-line log line so it's
	// readable in the terminal.
	for _, line := range strings.Split(msg.Text, "\n") {
		slog.Info("email body", "line", line)
	}
	return nil
}
