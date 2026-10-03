package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration, loaded once at startup.
// Nothing reads os.Getenv directly outside this package.
type Config struct {
	Env     string
	BaseURL string
	// SupportEmail is shown in the app footer for complaints and help.
	SupportEmail string
	Sentry       SentryConfig
	HTTP         HTTPConfig
	DB           DBConfig
	Session      SessionConfig
	Email        EmailConfig
	Storage      StorageConfig
	Admin        AdminConfig
}

type AdminConfig struct {
	// Path is the URL prefix for the admin panel. Defaults to a random-looking
	// string to make discovery harder. Change it in production via env var.
	Path string
	// CookieName for the admin session. Must be different from the business
	// session cookie.
	CookieName string
	// SessionTTL is how long an admin session lasts. Shorter than business.
	SessionTTL time.Duration
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type DBConfig struct {
	// App role — subject to RLS
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	SSLMode  string
	MaxConns int32
	MinConns int32

	// Admin role — migrations only
	AdminUser     string
	AdminPassword string
}

// DSN returns the connection string for the app role.
func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s&pool_max_conns=%d&pool_min_conns=%d",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode, d.MaxConns, d.MinConns,
	)
}

// AdminDSN returns the connection string for the migration runner.
func (d DBConfig) AdminDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		d.AdminUser, d.AdminPassword, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

type SessionConfig struct {
	CookieName     string
	CSRFCookieName string
	TTL            time.Duration
	// IdleTTL caps session lifetime without activity. Zero disables
	// idle enforcement (absolute TTL still applies).
	IdleTTL time.Duration
}

// SentryConfig controls error monitoring. Empty DSN disables Sentry
// entirely (local dev default): the app runs exactly as before.
type SentryConfig struct {
	DSN         string
	Environment string
}

type EmailConfig struct {
	Mode string // "console" | "smtp"
	From string

	// SMTP settings — only used when Mode == "smtp"
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	SMTPImplicitTLS bool
}

type StorageConfig struct {
	Mode     string // "local" | "s3"
	LocalDir string
}

// Load reads configuration from environment variables.
// It fails fast on any missing or invalid required value.
func Load() (*Config, error) {
	c := &Config{
		Env:          getEnv("ORDORA_ENV", "development"),
		BaseURL:      getEnv("ORDORA_BASE_URL", "http://localhost:8080"),
		SupportEmail: getEnv("ORDORA_SUPPORT_EMAIL", "support@ordora.local"),
		Sentry: SentryConfig{
			DSN:         getEnv("ORDORA_SENTRY_DSN", ""),
			Environment: getEnv("ORDORA_SENTRY_ENVIRONMENT", getEnv("ORDORA_ENV", "development")),
		},
		HTTP: HTTPConfig{
			Addr:            getEnv("ORDORA_HTTP_ADDR", ":8080"),
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		DB: DBConfig{
			Host:          requireEnv("ORDORA_DB_HOST"),
			Port:          getEnv("ORDORA_DB_PORT", "5432"),
			Name:          requireEnv("ORDORA_DB_NAME"),
			User:          requireEnv("ORDORA_DB_USER"),
			Password:      requireEnv("ORDORA_DB_PASSWORD"),
			SSLMode:       getEnv("ORDORA_DB_SSLMODE", "disable"),
			MaxConns:      int32(getEnvInt("ORDORA_DB_MAX_CONNS", 20)),
			MinConns:      int32(getEnvInt("ORDORA_DB_MIN_CONNS", 2)),
			AdminUser:     requireEnv("ORDORA_DB_ADMIN_USER"),
			AdminPassword: requireEnv("ORDORA_DB_ADMIN_PASSWORD"),
		},
		Session: SessionConfig{
			CookieName:     getEnv("ORDORA_SESSION_COOKIE_NAME", "ordora_session"),
			CSRFCookieName: getEnv("ORDORA_CSRF_COOKIE_NAME", "ordora_csrf"),
			TTL:            time.Duration(getEnvInt("ORDORA_SESSION_TTL_HOURS", 720)) * time.Hour,
			IdleTTL:        time.Duration(getEnvInt("ORDORA_SESSION_IDLE_HOURS", 168)) * time.Hour,
		},
		Email: EmailConfig{
			Mode:            getEnv("ORDORA_EMAIL_MODE", "console"),
			From:            getEnv("ORDORA_EMAIL_FROM", "no-reply@ordora.local"),
			SMTPHost:        getEnv("ORDORA_SMTP_HOST", ""),
			SMTPPort:        getEnvInt("ORDORA_SMTP_PORT", 587),
			SMTPUsername:    getEnv("ORDORA_SMTP_USERNAME", ""),
			SMTPPassword:    getEnv("ORDORA_SMTP_PASSWORD", ""),
			SMTPImplicitTLS: getEnvBool("ORDORA_SMTP_IMPLICIT_TLS", false),
		},
		Storage: StorageConfig{
			Mode:     getEnv("ORDORA_STORAGE_MODE", "local"),
			LocalDir: getEnv("ORDORA_STORAGE_LOCAL_DIR", "./uploads"),
		},
		Admin: AdminConfig{
			Path:       getEnv("ORDORA_ADMIN_PATH", "/ops-x9k2m"),
			CookieName: getEnv("ORDORA_ADMIN_SESSION_COOKIE", "ordora_admin_session"),
			SessionTTL: time.Duration(getEnvInt("ORDORA_ADMIN_SESSION_TTL_HOURS", 8)) * time.Hour,
		},
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.DB.Host == "" {
		return fmt.Errorf("config: ORDORA_DB_HOST is required")
	}
	if c.DB.Name == "" {
		return fmt.Errorf("config: ORDORA_DB_NAME is required")
	}
	if c.DB.User == "" || c.DB.Password == "" {
		return fmt.Errorf("config: ORDORA_DB_USER and ORDORA_DB_PASSWORD are required")
	}
	switch c.Email.Mode {
	case "console", "smtp":
	default:
		return fmt.Errorf("config: invalid ORDORA_EMAIL_MODE %q", c.Email.Mode)
	}
	if c.Email.Mode == "smtp" {
		if c.Email.SMTPHost == "" {
			return fmt.Errorf("config: ORDORA_SMTP_HOST is required when ORDORA_EMAIL_MODE=smtp")
		}
		if c.Email.SMTPPort == 0 {
			return fmt.Errorf("config: ORDORA_SMTP_PORT is required when ORDORA_EMAIL_MODE=smtp")
		}
	}
	switch c.Storage.Mode {
	case "local", "s3":
	default:
		return fmt.Errorf("config: invalid ORDORA_STORAGE_MODE %q", c.Storage.Mode)
	}
	return nil
}

func (c *Config) IsDev() bool { return c.Env == "development" }

// ---- helpers ----

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		panic(fmt.Sprintf("config: required environment variable %s is not set", key))
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}
