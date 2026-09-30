package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

func main() {
	// Load .env in dev; on the VPS, environment variables come from systemd.
	_ = godotenv.Load()

	// Structured JSON logs in production; text in dev for readability.
	setupLogging()

	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- Database ----
	db, err := postgres.Open(ctx, cfg.DB.DSN())
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	defer db.Close()
	slog.Info("db connected", "host", cfg.DB.Host, "name", cfg.DB.Name, "user", cfg.DB.User)

	// ---- Auth service (not yet used by HTTP; wired in M1.3b) ----
	authService := auth.NewService(auth.Deps{
		DB:       db,
		Users:    postgres.NewUserRepo(db),
		Orgs:     postgres.NewOrgRepo(db),
		Members:  postgres.NewMemberRepo(db),
		Sessions: postgres.NewSessionRepo(db),
		Tokens:   postgres.NewAuthTokenRepo(db),
		IDs:      id.Generator{},
		Mailer:   auth.LogMailer{},
	})
	_ = authService // silence unused; will be used in M1.3b

	// ---- Templates ----
	templatesDir := filepath.Join("internal", "web", "templates")
	renderer, err := render.New(templatesDir)
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}

	// ---- Router ----
	mux := http.NewServeMux()

	// Static files
	staticDir := filepath.Join("internal", "web", "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Home
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		renderer.Page(w, http.StatusOK, "layouts/app.html", "partials/home.html", map[string]any{
			"Title": "",
		})
	})

	// Health: liveness. Never touches the DB.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Ready: readiness. Pings the DB.
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"degraded","error":%q}`, err.Error())
			return
		}
		fmt.Fprint(w, `{"status":"ready"}`)
	})

	// ---- Middleware chain ----
	handler := chain(mux,
		middleware.Recover,
		middleware.RequestID,
		middleware.Logger,
	)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	// ---- Graceful shutdown ----
	errCh := make(chan error, 1)
	go func() {
		slog.Info("http listening", "addr", cfg.HTTP.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	slog.Info("stopped cleanly")
	return nil
}

// chain composes middleware right-to-left so the first argument is outermost.
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func setupLogging() {
	env := os.Getenv("ORDORA_ENV")
	var h slog.Handler
	if env == "production" {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	slog.SetDefault(slog.New(h))
}
