package auth

import (
	"context"
	"log/slog"

	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// LogMailer is a development Mailer that logs messages to stdout instead of
// sending them. Replace with an SMTP/provider implementation in production.
type LogMailer struct{}

func (LogMailer) SendVerifyEmail(_ context.Context, to user.Email, name, link string) error {
	slog.Info("mailer: verify email",
		"to", to.String(),
		"name", name,
		"link", link,
	)
	return nil
}

func (LogMailer) SendPasswordReset(_ context.Context, to user.Email, name, link string) error {
	slog.Info("mailer: password reset",
		"to", to.String(),
		"name", name,
		"link", link,
	)
	return nil
}
