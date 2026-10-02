package email

import (
	"fmt"
	"log/slog"

	"github.com/Adeyinka7789/ordora/internal/config"
)

// NewFromConfig builds a Mailer from config.
//
//   - "console" → ConsoleMailer (dev)
//   - "smtp"    → SMTPMailer (production)
//
// Returns an error if the mode is unknown or config is incomplete.
func NewFromConfig(cfg config.EmailConfig) (Mailer, error) {
	switch cfg.Mode {
	case "console":
		slog.Info("email: using console mailer (development)")
		return NewConsoleMailer(), nil

	case "smtp":
		m, err := NewSMTPMailer(SMTPConfig{
			Host:        cfg.SMTPHost,
			Port:        cfg.SMTPPort,
			Username:    cfg.SMTPUsername,
			Password:    cfg.SMTPPassword,
			From:        cfg.From,
			ImplicitTLS: cfg.SMTPImplicitTLS,
		})
		if err != nil {
			return nil, fmt.Errorf("email: %w", err)
		}
		slog.Info("email: using SMTP mailer",
			"host", cfg.SMTPHost,
			"port", cfg.SMTPPort,
			"implicit_tls", cfg.SMTPImplicitTLS,
		)
		return m, nil

	default:
		return nil, fmt.Errorf("email: unknown mode %q", cfg.Mode)
	}
}
