package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPConfig configures an SMTPMailer.
type SMTPConfig struct {
	Host     string // e.g. "smtp.example.com" or "localhost"
	Port     int    // 25, 465, or 587
	Username string // optional for local relay
	Password string // optional for local relay
	From     string // e.g. "Ordora <no-reply@ordora.com>"
	// Timeout for the whole send operation. Defaults to 20s.
	Timeout time.Duration
	// ImplicitTLS forces SMTPS (usually port 465). When false, the mailer
	// attempts STARTTLS if the server offers it.
	ImplicitTLS bool
}

// SMTPMailer sends email via an SMTP server.
type SMTPMailer struct {
	cfg SMTPConfig
}

// NewSMTPMailer constructs an SMTPMailer. It does not connect at
// construction time — the first send will establish the connection.
func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	if cfg.Host == "" {
		return nil, errors.New("email: SMTP host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" {
		return nil, errors.New("email: SMTP from address is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 20 * time.Second
	}
	return &SMTPMailer{cfg: cfg}, nil
}

// Send delivers a message.
func (s *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if err := validate(msg); err != nil {
		return err
	}

	fromHeader, fromAddr, err := parseFrom(s.cfg.From)
	if err != nil {
		return err
	}

	raw, err := buildRawMessage(msg, fromHeader, msg.To)
	if err != nil {
		return fmt.Errorf("email: build message: %w", err)
	}

	// Apply a deadline to the whole operation.
	deadline := time.Now().Add(s.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprintf("%d", s.cfg.Port))

	// Dial.
	var conn net.Conn
	if s.cfg.ImplicitTLS {
		dialer := &net.Dialer{Timeout: 10 * time.Second, Deadline: deadline}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: s.cfg.Host,
		})
	} else {
		dialer := &net.Dialer{Timeout: 10 * time.Second, Deadline: deadline}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("email: dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("email: new client: %w", err)
	}
	defer client.Close()

	// STARTTLS if not already TLS and the server supports it.
	if !s.cfg.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
				return fmt.Errorf("email: starttls: %w", err)
			}
		}
	}

	// AUTH if credentials are provided.
	if s.cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("email: auth: %w", err)
			}
		}
	}

	// Envelope.
	if err := client.Mail(fromAddr); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("email: rcpt to: %w", err)
	}

	// Body.
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("email: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close data: %w", err)
	}

	_ = client.Quit()
	return nil
}

// parseFrom splits "Display Name <addr@domain>" into (display, addr).
// If there's no display, returns (addr, addr).
func parseFrom(from string) (display, addr string, err error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", errors.New("email: empty from")
	}
	// Simple form: just an address.
	if !strings.Contains(from, "<") {
		return from, from, nil
	}
	// Display Name <addr> form.
	open := strings.Index(from, "<")
	close := strings.Index(from, ">")
	if open < 0 || close < 0 || close < open {
		return "", "", fmt.Errorf("email: malformed from %q", from)
	}
	display = strings.TrimSpace(from[:open])
	addr = strings.TrimSpace(from[open+1 : close])
	if addr == "" {
		return "", "", fmt.Errorf("email: empty from addr in %q", from)
	}
	return display, addr, nil
}

// buildRawMessage produces a MIME multipart/alternative message with both
// plain-text and HTML bodies.
func buildRawMessage(msg Message, fromHeader, toHeader string) ([]byte, error) {
	const boundary = "ordora-boundary-0x4d3a9b2f"
	var b strings.Builder

	writeHeader := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}

	writeHeader("From", fromHeader)
	writeHeader("To", toHeader)
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	writeHeader("Content-Type", "multipart/alternative; boundary="+boundary)

	b.WriteString("\r\n")

	// Plain text part.
	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	text := msg.Text
	if text == "" {
		text = stripHTML(msg.HTML)
	}
	b.WriteString(text)
	b.WriteString("\r\n")

	// HTML part.
	if msg.HTML != "" {
		b.WriteString("--")
		b.WriteString(boundary)
		b.WriteString("\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(msg.HTML)
		b.WriteString("\r\n")
	}

	// Close boundary.
	b.WriteString("--")
	b.WriteString(boundary)
	b.WriteString("--\r\n")

	return []byte(b.String()), nil
}

// stripHTML is a last-resort plain-text fallback. Not perfect; if we care
// about quality we always supply msg.Text explicitly.
func stripHTML(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
