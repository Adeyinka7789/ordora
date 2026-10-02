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

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/infra/email"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/infra/storage"
	"github.com/Adeyinka7789/ordora/internal/jobs"
	"github.com/Adeyinka7789/ordora/internal/web/handlers"
	"github.com/Adeyinka7789/ordora/internal/web/middleware"
	"github.com/Adeyinka7789/ordora/internal/web/render"
)

func main() {
	_ = godotenv.Load()
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

	// ---- Email ----
	mailer, err := email.NewFromConfig(cfg.Email)
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}

	emailRenderer, err := email.NewRenderer()
	if err != nil {
		return fmt.Errorf("email renderer: %w", err)
	}

	// In development, run the background workers as goroutines so you
	// don't have to manage two terminals. In production, cmd/worker runs
	// them separately.
	if cfg.IsDev() {
		relay := jobs.NewOutboxRelay(db, jobs.OutboxRelayConfig{})
		notifWorker := jobs.NewNotificationWorker(db, emailRenderer, mailer, jobs.NotificationWorkerConfig{})
		go relay.Run(ctx)
		go notifWorker.Run(ctx)
		slog.Info("dev: background workers running in-process")
	}
	_ = mailer // used by services

	// ---- Auth service ----
	authMailer := email.NewAuthMailer(mailer, emailRenderer)
	authService := auth.NewService(auth.Deps{
		DB:       db,
		Users:    postgres.NewUserRepo(db),
		Orgs:     postgres.NewOrgRepo(db),
		Members:  postgres.NewMemberRepo(db),
		Sessions: postgres.NewSessionRepo(db),
		Tokens:   postgres.NewAuthTokenRepo(db),
		IDs:      id.Generator{},
		Mailer:   authMailer,
	})

	// ---- Templates ----
	templatesDir := filepath.Join("internal", "web", "templates")
	renderer, err := render.New(templatesDir)
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}

	// ---- Handlers ----
	authH := &handlers.AuthHandler{
		Auth:     authService,
		Renderer: renderer,
		Cfg:      cfg,
	}
	dashService := app.NewDashboardService(db)
	dashH := &handlers.DashboardHandler{
		Service:  dashService,
		Renderer: renderer,
	}

	custRepo := postgres.NewCustomerRepo(db)
	customerH := handlers.NewCustomerHandler(custRepo, renderer)

	// ---- Services ----
	outboxRepo := postgres.NewOutboxRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	orderRepo := postgres.NewOrderRepo(db)
	// ---- Attachments ----
	blobs, err := storage.NewLocalFS(cfg.Storage.LocalDir)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	attachRepo := postgres.NewAttachmentRepo(db)
	attachService := app.NewAttachmentService(app.AttachmentServiceDeps{
		Blobs: blobs,
		Repo:  attachRepo,
		IDs:   id.Generator{},
	})

	numberRepo := postgres.NewOrderNumberRepo(db)

	orderService := app.NewOrderService(app.OrderServiceDeps{
		DB:        db,
		Customers: custRepo,
		Orders:    orderRepo,
		OrderRead: orderRepo,
		Numbers:   numberRepo,
		Audit:     auditRepo,
		Outbox:    outboxRepo,
		IDs:       id.Generator{},
	})

	paymentRepo := postgres.NewPaymentRepo(db)
	paymentService := app.NewPaymentService(app.PaymentServiceDeps{
		DB:          db,
		Payments:    paymentRepo,
		PaymentRead: paymentRepo,
		Orders:      orderRepo,
		Audit:       auditRepo,
		Outbox:      outboxRepo,
		IDs:         id.Generator{},
	})
	paymentH := &handlers.PaymentHandler{
		Service:     paymentService,
		Orders:      orderService,
		Attachments: attachService,
		Renderer:    renderer,
	}

	attachH := &handlers.AttachmentHandler{
		Service:       attachService,
		PaymentLookup: paymentRepo,
		OrderService:  orderService,
		PaymentSvc:    paymentService,
		Renderer:      renderer,
	}

	// ---- Products ----
	productRepo := postgres.NewProductRepo(db)
	productService := app.NewProductService(app.ProductServiceDeps{
		Store: productRepo,
		IDs:   id.Generator{},
	})
	productH := &handlers.ProductHandler{
		Service:  productService,
		Repo:     productRepo,
		Renderer: renderer,
	}

	orderH := &handlers.OrderHandler{
		Service:     orderService,
		OrderRepo:   orderRepo,
		CustRepo:    custRepo,
		Attachments: attachService,
		Payments:    paymentService,
		Audit:       auditRepo,
		Renderer:    renderer,
	}

	stubH := &handlers.StubHandler{Renderer: renderer}

	// ---- Public portal ----
	portalRepo := postgres.NewPortalRepo(db)
	portalService := app.NewPortalService(app.PortalServiceDeps{
		Repo: portalRepo,
	})
	publicOrderRepo := postgres.NewPublicOrderRepo(db, id.Generator{})
	publicOrderService := app.NewPublicOrderService(app.PublicOrderServiceDeps{
		DB:     publicOrderRepo,
		Outbox: outboxRepo,
		IDs:    id.Generator{},
	})
	portalH := &handlers.PortalHandler{
		Portal:   portalService,
		Public:   publicOrderService,
		Renderer: renderer,
	}

	// ---- Router ----
	mux := http.NewServeMux()

	// Static
	staticDir := filepath.Join("internal", "web", "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Home
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s := middleware.SessionFromContext(r.Context())
		if s != nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	// ---- Public routes (no auth required) ----
	portalMux := http.NewServeMux()
	portalMux.HandleFunc("GET /o/{token}", portalH.Show)
	portalMux.HandleFunc("GET /order/{slug}", portalH.IntakeForm)
	portalMux.HandleFunc("POST /order/{slug}", portalH.IntakeSubmit)

	// Wire portal routes into the main mux, with rate limiting on POST only.
	// (GET routes are cheap; the POST is what needs protection.)
	mux.Handle("GET /o/{token}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 60, Window: time.Minute,
		Message: "Too many requests. Please slow down.",
	})(portalMux))
	mux.Handle("GET /order/{slug}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 60, Window: time.Minute,
		Message: "Too many requests. Please slow down.",
	})(portalMux))
	mux.Handle("POST /order/{slug}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 5, Window: time.Minute,
		Message: "Too many submission attempts. Please wait a minute.",
	})(portalMux))

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})
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

	// ---- Auth routes ----
	mux.HandleFunc("GET /register", authH.RegisterPage)
	mux.HandleFunc("POST /register", authH.Register)
	mux.HandleFunc("GET /login", authH.LoginPage)
	mux.HandleFunc("POST /login", authH.Login)
	mux.HandleFunc("POST /logout", authH.Logout)
	mux.HandleFunc("GET /verify", authH.VerifyEmail)
	mux.HandleFunc("GET /password/forgot", authH.ForgotPage)
	mux.HandleFunc("POST /password/forgot", authH.Forgot)
	mux.HandleFunc("GET /password/reset", authH.ResetPage)
	mux.HandleFunc("POST /password/reset", authH.Reset)

	// ---- Dashboard (requires auth) ----
	mux.Handle("GET /dashboard", middleware.RequireAuth(http.HandlerFunc(dashH.Index)))

	// ---- Customers (requires auth + tenant) ----
	mux.Handle("GET /customers", middleware.RequireTenant(http.HandlerFunc(customerH.Index)))
	mux.Handle("GET /customers/new", middleware.RequireTenant(http.HandlerFunc(customerH.New)))
	mux.Handle("POST /customers", middleware.RequireTenant(http.HandlerFunc(customerH.Create)))
	mux.Handle("GET /customers/{id}", middleware.RequireTenant(http.HandlerFunc(customerH.Show)))
	mux.Handle("GET /customers/{id}/edit", middleware.RequireTenant(http.HandlerFunc(customerH.Edit)))
	mux.Handle("POST /customers/{id}", middleware.RequireTenant(http.HandlerFunc(customerH.Update)))
	mux.Handle("POST /customers/{id}/delete", middleware.RequireTenant(http.HandlerFunc(customerH.Delete)))
	// ---- Stub pages (nav links that are on the roadmap) ----
	mux.Handle("GET /reports", middleware.RequireTenant(http.HandlerFunc(stubH.Reports)))
	mux.Handle("GET /settings", middleware.RequireTenant(http.HandlerFunc(stubH.Settings)))
	mux.Handle("GET /payments", middleware.RequireTenant(http.HandlerFunc(stubH.Payments)))
	mux.Handle("GET /storefront", middleware.RequireTenant(http.HandlerFunc(stubH.Storefront)))
	// ---- Middleware chain ----
	// ---- Products ----
	mux.Handle("GET /products", middleware.RequireTenant(http.HandlerFunc(productH.Index)))
	mux.Handle("GET /products/new", middleware.RequireTenant(http.HandlerFunc(productH.New)))
	mux.Handle("POST /products", middleware.RequireTenant(http.HandlerFunc(productH.Create)))
	mux.Handle("GET /products/picker", middleware.RequireTenant(http.HandlerFunc(productH.Picker)))
	mux.Handle("GET /products/{id}", middleware.RequireTenant(http.HandlerFunc(productH.Show)))
	mux.Handle("GET /products/{id}/edit", middleware.RequireTenant(http.HandlerFunc(productH.Edit)))
	mux.Handle("POST /products/{id}", middleware.RequireTenant(http.HandlerFunc(productH.Update)))
	mux.Handle("POST /products/{id}/archive", middleware.RequireTenant(http.HandlerFunc(productH.Archive)))
	//
	// Order (outermost to innermost):
	//   Recover -> RequestID -> Logger -> Session -> CSRF -> mux

	// ---- Orders (requires auth + tenant) ----
	mux.Handle("GET /orders", middleware.RequireTenant(http.HandlerFunc(orderH.Index)))
	mux.Handle("GET /orders/new", middleware.RequireTenant(http.HandlerFunc(orderH.New)))
	mux.Handle("POST /orders", middleware.RequireTenant(http.HandlerFunc(orderH.Create)))
	mux.Handle("GET /orders/{id}", middleware.RequireTenant(http.HandlerFunc(orderH.Show)))
	mux.Handle("GET /orders/{id}/edit", middleware.RequireTenant(http.HandlerFunc(orderH.Edit)))
	mux.Handle("POST /orders/{id}", middleware.RequireTenant(http.HandlerFunc(orderH.Update)))
	mux.Handle("POST /orders/{id}/status", middleware.RequireTenant(http.HandlerFunc(orderH.ChangeStatus)))
	mux.Handle("POST /orders/{id}/public-token/regenerate", middleware.RequireTenant(http.HandlerFunc(orderH.RegenerateToken)))
	//
	// ---- Payments ----
	mux.Handle("POST /orders/{id}/payments", middleware.RequireTenant(http.HandlerFunc(paymentH.Record)))
	mux.Handle("POST /payments/{id}/attachments", middleware.RequireTenant(http.HandlerFunc(attachH.UploadToPayment)))
	mux.Handle("POST /payments/{id}/reverse", middleware.RequireTenant(http.HandlerFunc(paymentH.Reverse)))

	// ---- Attachments ----
	mux.Handle("POST /orders/{id}/attachments", middleware.RequireTenant(http.HandlerFunc(attachH.UploadToOrder)))
	mux.Handle("GET /attachments/{id}", middleware.RequireTenant(http.HandlerFunc(attachH.Download)))
	// Session must run before CSRF (CSRF does not need it but templates do).
	// Session must run before any handler that reads the context.
	handler := chain(mux,
		middleware.Recover,
		middleware.RequestID,
		middleware.Logger,
		middleware.SessionMiddleware(authService, cfg.Session.CookieName),
		middleware.CSRF(middleware.CSRFConfig{
			CookieName: cfg.Session.CSRFCookieName,
			Secure:     !cfg.IsDev(),
		}),
	)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

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
