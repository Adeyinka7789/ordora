package email

import (
	"context"

	"github.com/Adeyinka7789/ordora/internal/domain/user"
)

// AuthMailer adapts email.Mailer + Renderer to the auth.Mailer interface.
// Used by the auth service to send verification and password-reset emails.
type AuthMailer struct {
	sender   Mailer
	renderer *Renderer
}

func NewAuthMailer(sender Mailer, renderer *Renderer) *AuthMailer {
	return &AuthMailer{sender: sender, renderer: renderer}
}

func (m *AuthMailer) SendVerifyEmail(ctx context.Context, to user.Email, name, link string) error {
	msg, err := m.renderer.Render("welcome_verify", "Verify your Ordora email", to.String(), map[string]any{
		"Name":      name,
		"VerifyURL": link,
		"OrgName":   "Ordora",
	})
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}

func (m *AuthMailer) SendPasswordReset(ctx context.Context, to user.Email, name, link string) error {
	msg, err := m.renderer.Render("password_reset", "Reset your Ordora password", to.String(), map[string]any{
		"Name":     name,
		"ResetURL": link,
	})
	if err != nil {
		return err
	}
	return m.sender.Send(ctx, msg)
}
