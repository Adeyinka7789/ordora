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

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"

	"github.com/Adeyinka7789/ordora/internal/app"
	"github.com/Adeyinka7789/ordora/internal/auth"
	"github.com/Adeyinka7789/ordora/internal/config"
	"github.com/Adeyinka7789/ordora/internal/contact"
	"github.com/Adeyinka7789/ordora/internal/domain/org"
	"github.com/Adeyinka7789/ordora/internal/domain/tenant"
	"github.com/Adeyinka7789/ordora/internal/domain/user"
	"github.com/Adeyinka7789/ordora/internal/flags"
	"github.com/Adeyinka7789/ordora/internal/infra/email"
	"github.com/Adeyinka7789/ordora/internal/infra/id"
	"github.com/Adeyinka7789/ordora/internal/infra/postgres"
	"github.com/Adeyinka7789/ordora/internal/infra/storage"
	"github.com/Adeyinka7789/ordora/internal/jobs"
	"github.com/Adeyinka7789/ordora/internal/observe"
	"github.com/Adeyinka7789/ordora/internal/web/handlers"
	adminhandlers "github.com/Adeyinka7789/ordora/internal/web/handlers/admin"
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

	// Error monitoring. Empty DSN (local dev) disables Sentry entirely.
	if err := observe.Init(observe.Options{
		DSN:              cfg.Sentry.DSN,
		Environment:      cfg.Sentry.Environment,
		TracesSampleRate: cfg.Sentry.TracesSampleRate,
		EnableLogs:       cfg.Sentry.EnableLogs,
	}); err != nil {
		return fmt.Errorf("sentry: %w", err)
	}
	defer observe.Flush()

	// One-shot "It works!" ping: ORDORA_SENTRY_VERIFY=1 sends a test
	// message at startup (then unset it — every boot would spam Issues).
	if cfg.Sentry.Verify {
		if observe.Verify() {
			slog.Info("sentry: verify ping sent — check Issues")
		} else {
			slog.Warn("sentry: verify requested but Sentry is disabled")
		}
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

	// ---- Admin database pool ----
	// A separate pool for admin queries. Connects as ordora_admin (BYPASSRLS)
	// so cross-tenant queries work. Never used by business requests.
	adminDB, err := postgres.Open(ctx, cfg.DB.AdminDSN())
	if err != nil {
		return fmt.Errorf("admin db: %w", err)
	}
	defer adminDB.Close()

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
		observe.SafeGo("outbox-relay", func() { relay.Run(ctx) })
		observe.SafeGo("notifications", func() { notifWorker.Run(ctx) })
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
		Attempts: postgres.NewLoginAttemptRepo(db),
		IDs:      id.Generator{},
		Mailer:   authMailer,
		IdleTTL:  cfg.Session.IdleTTL,
	})

	// ---- Templates ----
	templatesDir := filepath.Join("internal", "web", "templates")
	renderer, err := render.New(templatesDir)
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}
	render.SupportEmail = cfg.SupportEmail

	// Editable public contact details (utility top bar + WhatsApp float).
	// Missing file hides the details; invalid JSON fails fast.
	if cinfo, err := contact.Load(cfg.ContactFile); err != nil {
		return fmt.Errorf("contact: %w", err)
	} else {
		render.Contact = cinfo
	}
	if _, err := os.Stat(cfg.ContactFile); err != nil {
		slog.Warn("contact: file not found, utility bar and WhatsApp button hidden",
			"file", cfg.ContactFile)
	} else if !render.Contact.HasAny() {
		slog.Warn("contact: file has no usable details (want keys: emails, phones, address, website, website_url, whatsapp_number, whatsapp_message)",
			"file", cfg.ContactFile)
	}

	// ---- Feature flags (Waffle-style) ----
	// Fail-closed in-memory snapshot, refreshed every 30s; admin writes
	// bust the cache immediately in-process.
	flagRepo := postgres.NewFlagRepo(db)
	flagProvider := flags.NewProvider(func(ctx context.Context) ([]flags.Flag, error) {
		return flagRepo.List(ctx)
	}, 30*time.Second).WithOverrideLoader(func(ctx context.Context) ([]flags.Override, error) {
		return flagRepo.ListOverrides(ctx)
	})
	if err := flagProvider.Refresh(ctx); err != nil {
		slog.Warn("flags: initial load failed; flags evaluate closed until refresh succeeds", "err", err)
	}
	go flagProvider.Start(ctx)
	render.Flags = flagProvider

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

	// ---- Services ----
	outboxRepo := postgres.NewOutboxRepo(db)
	auditRepo := postgres.NewAuditRepo(db)
	orderRepo := postgres.NewOrderRepo(db)

	customerH := handlers.NewCustomerHandler(custRepo, orderRepo, renderer)
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
	measurementRepo := postgres.NewMeasurementRepo(db)

	orderService := app.NewOrderService(app.OrderServiceDeps{
		DB:           db,
		Customers:    custRepo,
		Orders:       orderRepo,
		OrderRead:    orderRepo,
		Numbers:      numberRepo,
		Audit:        auditRepo,
		Outbox:       outboxRepo,
		Measurements: measurementRepo,
		IDs:          id.Generator{},
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
	ledgerH := &handlers.LedgerHandler{
		Repo:     paymentRepo,
		Flags:    flagProvider,
		Renderer: renderer,
	}

	// ---- Costs ----
	costRepo := postgres.NewCostRepo(db)
	costService := app.NewCostService(app.CostServiceDeps{
		Costs:  costRepo,
		Orders: orderRepo,
		IDs:    id.Generator{},
	})
	costH := &handlers.CostHandler{
		Service:  costService,
		Renderer: renderer,
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
		Service:      orderService,
		OrderRepo:    orderRepo,
		CustRepo:     custRepo,
		Orgs:         postgres.NewOrgRepo(db),
		Measurements: measurementRepo,
		Attachments:  attachService,
		Payments:     paymentService,
		Audit:        auditRepo,
		Renderer:     renderer,
		Costs:        costService,
		Groups:       postgres.NewGroupRepo(db),
	}

	// ---- Aso-ebi groups + calendar ----
	groupRepo := postgres.NewGroupRepo(db)
	groupService := app.NewGroupService(app.GroupServiceDeps{
		DB:     db,
		Groups: groupRepo,
		Audit:  auditRepo,
		IDs:    id.Generator{},
	})
	groupH := &handlers.GroupHandler{
		Service:  groupService,
		Repo:     groupRepo,
		Orders:   orderRepo,
		Measure:  measurementRepo,
		Renderer: renderer,
	}
	groupPublicH := &handlers.GroupPublicHandler{
		Groups:   groupRepo,
		Measure:  measurementRepo,
		Renderer: renderer,
	}
	calendarH := &handlers.CalendarHandler{
		Orders:   orderRepo,
		Groups:   groupRepo,
		Renderer: renderer,
	}

	// ---- Settings + profile ----
	settingsService := app.NewSettingsService(app.SettingsServiceDeps{
		DB:       db,
		Orgs:     postgres.NewOrgRepo(db),
		Users:    postgres.NewUserRepo(db),
		Sessions: postgres.NewSessionRepo(db),
		Audit:    auditRepo,
	})
	settingsH := &handlers.SettingsHandler{
		Service:  settingsService,
		Renderer: renderer,
	}
	measTmplH := &handlers.MeasurementTemplateHandler{
		Repo:     measurementRepo,
		Orgs:     postgres.NewOrgRepo(db),
		Renderer: renderer,
	}

	storefrontH := &handlers.StorefrontHandler{
		Orgs:     postgres.NewOrgRepo(db),
		Renderer: renderer,
	}

	// ---- Search ----
	searchRepo := postgres.NewSearchRepo(db)
	searchSvc := app.NewSearchService(app.SearchServiceDeps{
		Repo: postgres.NewSearchAdapter(searchRepo),
	})
	searchH := &handlers.SearchHandler{
		Service:  searchSvc,
		Renderer: renderer,
	}

	// ---- Reports ----
	reportRepo := postgres.NewReportRepo(db)
	reportSvc := app.NewReportService(app.ReportServiceDeps{
		Repo: postgres.NewReportAdapter(reportRepo),
	})
	reportH := &handlers.ReportHandler{
		Service:  reportSvc,
		Renderer: renderer,
	}

	// ---- Admin panel ----
	adminAuthService := auth.NewAdminAuthService(auth.AdminAuthDeps{
		Admins:   postgres.NewPlatformAdminRepo(db),
		Sessions: postgres.NewAdminSessionRepo(db),
		IDs:      id.Generator{},
		TTL:      cfg.Admin.SessionTTL,
	})
	adminAuthH := &adminhandlers.AuthHandler{
		Auth:     adminAuthService,
		Renderer: renderer,
		Cfg:      cfg,
	}
	adminDashH := &adminhandlers.DashboardHandler{
		Renderer: renderer,
		Cfg:      cfg,
	}

	// ---- Admin service + extended handlers ----
	adminOrgRepo := postgres.NewAdminOrgRepo(adminDB)
	adminUserRepo := postgres.NewAdminUserRepo(adminDB)
	adminImpersonationRepo := postgres.NewAdminImpersonationRepo(adminDB)
	adminAuditRepo := postgres.NewAdminAuditRepo(adminDB)

	adminSvc := app.NewAdminService(app.AdminServiceDeps{
		Orgs:           postgres.NewAdminOrgAdapter(adminOrgRepo),
		Users:          postgres.NewAdminUserAdapter(adminUserRepo),
		Impersonations: adminImpersonationRepo,
		Audit:          postgres.NewAdminAuditAdapter(adminAuditRepo),
		IDs:            id.Generator{},
	})

	adminOrgH := &adminhandlers.OrgHandler{
		Service:  adminSvc,
		Flags:    postgres.NewFlagRepo(adminDB),
		Provider: flagProvider,
		Audit:    postgres.NewAdminAuditAdapter(adminAuditRepo),
		Renderer: renderer,
		Cfg:      cfg,
	}
	adminUserH := &adminhandlers.UserHandler{
		Service:  adminSvc,
		Attempts: postgres.NewLoginAttemptRepo(adminDB),
		Audit:    postgres.NewAdminAuditAdapter(adminAuditRepo),
		Renderer: renderer,
		Cfg:      cfg,
	}
	adminImpersonateH := &adminhandlers.ImpersonateHandler{
		Service:  adminSvc,
		Renderer: renderer,
		Cfg:      cfg,
	}

	adminDataRepo := postgres.NewAdminDataRepo(adminDB)
	adminDataSvc := app.NewAdminDataService(app.AdminDataServiceDeps{
		Repo:  postgres.NewAdminDataAdapter(adminDataRepo),
		Audit: postgres.NewAdminAuditAdapter(adminAuditRepo),
	})
	adminDataH := &adminhandlers.DataHandler{
		Service:  adminDataSvc,
		Renderer: renderer,
		Cfg:      cfg,
	}

	adminLookupRepo := postgres.NewAdminLookupRepo(adminDB)
	adminLookupSvc := app.NewAdminLookupService(app.AdminLookupServiceDeps{
		Repo:  postgres.NewAdminLookupAdapter(adminLookupRepo),
		Audit: postgres.NewAdminAuditAdapter(adminAuditRepo),
	})
	adminLookupH := &adminhandlers.LookupHandler{
		Service:  adminLookupSvc,
		Renderer: renderer,
		Cfg:      cfg,
	}

	adminOpsRepo := postgres.NewAdminOpsRepo(adminDB, db)
	adminOpsSvc := app.NewAdminOpsService(app.AdminOpsServiceDeps{
		Repo: postgres.NewAdminOpsAdapter(adminOpsRepo, adminAuditRepo),
	})
	adminOpsH := &adminhandlers.OpsHandler{
		Service:  adminOpsSvc,
		Renderer: renderer,
		Cfg:      cfg,
	}

	// ---- In-app notifications + support desk ----
	// Tenant reads/writes go through the tenant DB (RLS); admin
	// cross-tenant work goes through the admin DB (BYPASSRLS).
	commsService := app.NewCommsService(app.CommsServiceDeps{
		DB:              db,
		AdminDB:         adminDB,
		Notifs:          postgres.NewNotificationRepo(db),
		Complaints:      postgres.NewComplaintRepo(db),
		AdminNotifs:     postgres.NewNotificationRepo(adminDB),
		AdminComplaints: postgres.NewComplaintRepo(adminDB),
		Audit:           postgres.NewAdminAuditAdapter(adminAuditRepo),
		IDs:             id.Generator{},
	})
	notifH := &handlers.NotificationHandler{
		Service:  commsService,
		Renderer: renderer,
	}
	supportH := &handlers.SupportHandler{
		Service:  commsService,
		Flags:    flagProvider,
		Renderer: renderer,
	}
	adminCommsH := &adminhandlers.CommsHandler{
		Service:  commsService,
		Renderer: renderer,
		Cfg:      cfg,
	}
	flagAdminH := &adminhandlers.FlagHandler{
		Flags:    postgres.NewFlagRepo(adminDB),
		Audit:    postgres.NewAdminAuditAdapter(adminAuditRepo),
		Provider: flagProvider,
		Renderer: renderer,
		Cfg:      cfg,
	}

	// Impersonation: build a synthetic ResolvedSession for a target org.
	// Used by the business session middleware when the impersonation cookie
	// is present. The synthetic session makes the admin appear as the org's
	// OWNER. Actions are attributed to the admin by the audit system.
	buildImpersonatedSession := func(ctx context.Context, orgID uuid.UUID) (*auth.ResolvedSession, error) {
		var ownerID uuid.UUID
		var ownerName, ownerEmail, orgName, orgSlug, orgCurrency, orgTimezone, orgCategory string

		err := adminDB.WithTx(ctx, func(tx pgx.Tx) error {
			const q = `
				SELECT m.user_id, u.name, u.email::text, o.name, o.slug::text, o.currency::text, o.timezone,
				       COALESCE(o.business_category,'')
				FROM organization_members m
				JOIN users u ON u.id = m.user_id
				JOIN organizations o ON o.id = m.organization_id
				WHERE m.organization_id = $1 AND m.role = 'OWNER' AND m.status = 'ACTIVE'
				ORDER BY m.created_at ASC
				LIMIT 1
			`
			return tx.QueryRow(ctx, q, orgID).Scan(&ownerID, &ownerName, &ownerEmail,
				&orgName, &orgSlug, &orgCurrency, &orgTimezone, &orgCategory)
		})
		if err != nil {
			return nil, err
		}

		// Minimal user object. We need Email + Name for the shell.
		email, _ := user.NewEmail(ownerEmail)
		u := &user.User{
			ID:    ownerID,
			Email: email,
			Name:  ownerName,
		}
		return &auth.ResolvedSession{
			User: u,
			Scope: tenant.TenantScope{
				OrgID:  orgID,
				UserID: ownerID,
				Role:   tenant.RoleOwner,
			},
			OrgName:     orgName,
			OrgSlug:     orgSlug,
			OrgCurrency: orgCurrency,
			OrgTimezone: orgTimezone,
			IsTailoring: org.IsTailoringCategory(orgCategory),
		}, nil
	}

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

	// gated wraps a tenant route with a feature-flag check. Disabled
	// modules 404 as if they don't exist.
	gated := func(flag string, h http.Handler) http.Handler {
		return middleware.RequireTenant(middleware.RequireFlag(flagProvider, flag)(h))
	}

	// gatedWrite is gated plus role authorization: VIEWER (read-only) users
	// get 403 on every mutation. All POST/DELETE business routes must use
	// this (or tenantWrite below) instead of gated.
	gatedWrite := func(flag string, h http.Handler) http.Handler {
		return middleware.RequireTenant(middleware.RequireWrite(middleware.RequireFlag(flagProvider, flag)(h)))
	}

	// tenantWrite wraps a tenant route that has no feature flag but mutates
	// data (settings, measurement templates). Same 403-for-VIEWER rule.
	tenantWrite := func(h http.Handler) http.Handler {
		return middleware.RequireTenant(middleware.RequireWrite(h))
	}

	// Per-IP limiter for credential-bearing POSTs (business + admin
	// login, registration, password reset). Brute-force protection;
	// account lockout additionally applies to business login.
	// In-memory per instance — documented in middleware.
	authLimit := middleware.RateLimit(middleware.RateLimitConfig{
		Limit:   10,
		Window:  time.Minute,
		Message: "Too many attempts. Please wait a minute and try again.",
	})

	// Offline fallback page for the PWA service worker.
	mux.HandleFunc("GET /offline", func(w http.ResponseWriter, r *http.Request) {
		renderer.RenderFragment(w, http.StatusOK, "errors/offline.html", nil)
	})

	// ---- Admin panel (configurable path) ----
	adminPath := cfg.Admin.Path
	adminSessionMW := middleware.AdminSessionMiddleware(adminAuthService, cfg.Admin.CookieName)

	mux.Handle("GET "+adminPath, adminSessionMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.AdminFromContext(r.Context()) != nil {
			http.Redirect(w, r, adminPath+"/dashboard", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, adminPath+"/login", http.StatusSeeOther)
	})))

	mux.Handle("GET "+adminPath+"/login", adminSessionMW(http.HandlerFunc(adminAuthH.LoginPage)))
	mux.Handle("POST "+adminPath+"/login", authLimit(adminSessionMW(http.HandlerFunc(adminAuthH.Login))))
	mux.HandleFunc("POST "+adminPath+"/logout", adminAuthH.Logout)

	mux.Handle("GET "+adminPath+"/dashboard",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminDashH.Index))))

	mux.Handle("GET "+adminPath+"/orgs",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.Index))))
	mux.Handle("GET "+adminPath+"/orgs/{id}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.Show))))
	mux.Handle("GET "+adminPath+"/users",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.Index))))
	mux.Handle("GET "+adminPath+"/users/{id}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.Show))))
	mux.Handle("POST "+adminPath+"/users/{id}/logout",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.ForceLogout))))
	mux.Handle("POST "+adminPath+"/users/{id}/lockout/clear",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.ClearLockout))))
	mux.Handle("POST "+adminPath+"/users/{id}/memberships/{orgID}/disable",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.DisableMember))))
	mux.Handle("POST "+adminPath+"/users/{id}/memberships/{orgID}/enable",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminUserH.EnableMember))))

	mux.Handle("POST "+adminPath+"/impersonate/{orgID}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminImpersonateH.Start))))
	mux.Handle("POST /impersonate/exit", http.HandlerFunc(adminImpersonateH.Stop))

	mux.Handle("GET "+adminPath+"/data",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminDataH.Tables))))
	mux.Handle("GET "+adminPath+"/data/{table}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminDataH.Rows))))

	mux.Handle("GET "+adminPath+"/lookup",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminLookupH.Index))))
	mux.Handle("POST "+adminPath+"/lookup/jobs/{id}/resend",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminLookupH.ResendJob))))
	mux.Handle("GET "+adminPath+"/ops",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOpsH.Ops))))
	mux.Handle("GET "+adminPath+"/audit",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOpsH.Audit))))

	// ---- Admin support desk + broadcasts ----
	mux.Handle("GET "+adminPath+"/complaints",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.ComplaintsIndex))))
	mux.Handle("GET "+adminPath+"/complaints/{id}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.ComplaintShow))))
	mux.Handle("POST "+adminPath+"/complaints/{id}/reply",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.ComplaintReply))))
	mux.Handle("POST "+adminPath+"/complaints/{id}/resolve",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.ComplaintResolve))))
	mux.Handle("POST "+adminPath+"/complaints/{id}/reopen",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.ComplaintReopen))))
	mux.Handle("GET "+adminPath+"/broadcasts",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.BroadcastsIndex))))
	mux.Handle("POST "+adminPath+"/broadcasts",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminCommsH.BroadcastCreate))))

	// ---- Admin feature flags ----
	mux.Handle("GET "+adminPath+"/flags",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(flagAdminH.Index))))
	mux.Handle("POST "+adminPath+"/flags/{key}/toggle",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(flagAdminH.Toggle))))
	mux.Handle("POST "+adminPath+"/flags/{key}",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(flagAdminH.Update))))

	mux.Handle("POST "+adminPath+"/orgs/{id}/suspend",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.Suspend))))
	mux.Handle("POST "+adminPath+"/orgs/{id}/unsuspend",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.Unsuspend))))
	mux.Handle("POST "+adminPath+"/orgs/{id}/delete",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.Delete))))
	mux.Handle("POST "+adminPath+"/orgs/{id}/flags/{key}/on",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.FlagOn))))
	mux.Handle("POST "+adminPath+"/orgs/{id}/flags/{key}/off",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.FlagOff))))
	mux.Handle("POST "+adminPath+"/orgs/{id}/flags/{key}/clear",
		adminSessionMW(middleware.RequireAdmin(adminPath+"/login")(http.HandlerFunc(adminOrgH.FlagClear))))

	// Static
	staticDir := filepath.Join("internal", "web", "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// Home
	// Home — landing page for visitors; redirect for signed-in users.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if s := middleware.SessionFromContext(r.Context()); s != nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		renderer.PagePublic(w, http.StatusOK, "layouts/landing.html", "landing/index.html", map[string]any{
			"Title": "Order management for growing businesses",
		})
	})
	// ---- Public routes (no auth required) ----
	portalMux := http.NewServeMux()
	portalMux.HandleFunc("GET /o/{token}", portalH.Show)
	portalMux.HandleFunc("GET /o/{token}/receipt", portalH.Receipt)
	portalMux.HandleFunc("GET /order/{slug}", portalH.IntakeForm)
	portalMux.HandleFunc("POST /order/{slug}", portalH.IntakeSubmit)

	mux.Handle("GET /search", middleware.RequireAuth(http.HandlerFunc(searchH.Handle)))

	// Wire portal routes into the main mux, with rate limiting on POST only.
	// (GET routes are cheap; the POST is what needs protection.)
	mux.Handle("GET /o/{token}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 60, Window: time.Minute,
		Message: "Too many requests. Please slow down.",
	})(portalMux))
	mux.Handle("GET /o/{token}/receipt", middleware.RateLimit(middleware.RateLimitConfig{
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
	// Credential-bearing POSTs are per-IP rate limited (brute-force
	// protection; account lockout additionally applies to login).
	// The limiter is in-memory per instance — documented in middleware.
	mux.HandleFunc("GET /register", authH.RegisterPage)
	mux.Handle("POST /register", authLimit(http.HandlerFunc(authH.Register)))
	mux.HandleFunc("GET /login", authH.LoginPage)
	mux.Handle("POST /login", authLimit(http.HandlerFunc(authH.Login)))
	mux.HandleFunc("POST /logout", authH.Logout)
	mux.HandleFunc("GET /verify", authH.VerifyEmail)
	mux.HandleFunc("GET /password/forgot", authH.ForgotPage)
	mux.Handle("POST /password/forgot", authLimit(http.HandlerFunc(authH.Forgot)))
	mux.HandleFunc("GET /password/reset", authH.ResetPage)
	mux.Handle("POST /password/reset", authLimit(http.HandlerFunc(authH.Reset)))

	// Silent handlers for well-known browser probes.
	mux.HandleFunc("GET /js/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, "// no-op service worker\n")
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.svg", http.StatusMovedPermanently)
	})

	// ---- Dashboard (requires auth) ----
	mux.Handle("GET /dashboard", middleware.RequireAuth(http.HandlerFunc(dashH.Index)))

	// ---- Onboarding wizard (first run only; handler redirects others) ----
	onboardH := &handlers.OnboardingHandler{Auth: authService, Renderer: renderer}
	mux.Handle("GET /onboarding", middleware.RequireAuth(http.HandlerFunc(onboardH.Show)))
	mux.Handle("POST /onboarding/complete", middleware.RequireAuth(http.HandlerFunc(onboardH.Complete)))
	mux.Handle("POST /onboarding/skip", middleware.RequireAuth(http.HandlerFunc(onboardH.Skip)))

	// ---- Customers (requires auth + tenant) ----
	mux.Handle("GET /customers", gated("customers", http.HandlerFunc(customerH.Index)))
	mux.Handle("GET /customers/new", gatedWrite("customers", http.HandlerFunc(customerH.New)))
	mux.Handle("POST /customers", gatedWrite("customers", http.HandlerFunc(customerH.Create)))
	mux.Handle("GET /customers/{id}", gated("customers", http.HandlerFunc(customerH.Show)))
	mux.Handle("GET /customers/{id}/edit", gatedWrite("customers", http.HandlerFunc(customerH.Edit)))
	mux.Handle("POST /customers/{id}", gatedWrite("customers", http.HandlerFunc(customerH.Update)))
	mux.Handle("POST /customers/{id}/delete", gatedWrite("customers", http.HandlerFunc(customerH.Delete)))

	// ---- Search  ----
	mux.Handle("GET /reports", gated("reports", http.HandlerFunc(reportH.Index)))
	mux.Handle("GET /reports/export.csv", gated("reports", http.HandlerFunc(reportH.ExportCSV)))

	// ---- Notifications + support desk (requires auth + tenant) ----
	mux.Handle("GET /notifications", middleware.RequireTenant(http.HandlerFunc(notifH.Index)))
	mux.Handle("GET /notifications/count", middleware.RequireTenant(http.HandlerFunc(notifH.Count)))
	mux.Handle("POST /notifications/{id}/read", middleware.RequireTenant(http.HandlerFunc(notifH.Read)))
	mux.Handle("POST /notifications/read-all", middleware.RequireTenant(http.HandlerFunc(notifH.ReadAll)))
	mux.Handle("GET /support", middleware.RequireTenant(http.HandlerFunc(supportH.Index)))
	mux.Handle("POST /support", middleware.RequireTenant(http.HandlerFunc(supportH.Create)))
	mux.Handle("GET /support/{id}", middleware.RequireTenant(http.HandlerFunc(supportH.Show)))
	mux.Handle("POST /support/{id}/reply", middleware.RequireTenant(http.HandlerFunc(supportH.Reply)))

	// ---- Stub pages (nav links that are on the roadmap) ----
	mux.Handle("GET /payments", gated("ledger", http.HandlerFunc(ledgerH.Index)))
	mux.Handle("GET /payments/export.csv", gated("ledger", http.HandlerFunc(ledgerH.ExportCSV)))
	mux.Handle("GET /storefront", middleware.RequireTenant(http.HandlerFunc(storefrontH.Index)))

	// ---- Settings + profile ----
	mux.Handle("GET /settings", middleware.RequireTenant(http.HandlerFunc(settingsH.Settings)))
	mux.Handle("POST /settings", tenantWrite(http.HandlerFunc(settingsH.UpdateSettings)))
	mux.Handle("GET /settings/measurements", middleware.RequireTenant(http.HandlerFunc(measTmplH.Index)))
	mux.Handle("GET /settings/measurements/new", tenantWrite(http.HandlerFunc(measTmplH.New)))
	mux.Handle("POST /settings/measurements", tenantWrite(http.HandlerFunc(measTmplH.Create)))
	mux.Handle("GET /settings/measurements/{id}/edit", tenantWrite(http.HandlerFunc(measTmplH.Edit)))
	mux.Handle("POST /settings/measurements/{id}", tenantWrite(http.HandlerFunc(measTmplH.Update)))
	mux.Handle("POST /settings/measurements/{id}/delete", tenantWrite(http.HandlerFunc(measTmplH.Delete)))
	mux.Handle("GET /profile", middleware.RequireAuth(http.HandlerFunc(settingsH.Profile)))
	mux.Handle("POST /profile", middleware.RequireAuth(http.HandlerFunc(settingsH.UpdateProfile)))
	mux.Handle("POST /profile/password", middleware.RequireAuth(http.HandlerFunc(settingsH.ChangePassword)))
	mux.Handle("POST /profile/sign-out-everywhere", middleware.RequireAuth(http.HandlerFunc(settingsH.SignOutEverywhere)))
	mux.Handle("POST /profile/sessions/{id}/revoke", middleware.RequireAuth(http.HandlerFunc(settingsH.RevokeSession)))

	// ---- Middleware chain ----
	// ---- Products ----
	mux.Handle("GET /products", gated("products", http.HandlerFunc(productH.Index)))
	mux.Handle("GET /products/new", gatedWrite("products", http.HandlerFunc(productH.New)))
	mux.Handle("POST /products", gatedWrite("products", http.HandlerFunc(productH.Create)))
	mux.Handle("GET /products/picker", gated("products", http.HandlerFunc(productH.Picker)))
	mux.Handle("GET /products/{id}", gated("products", http.HandlerFunc(productH.Show)))
	mux.Handle("GET /products/{id}/edit", gatedWrite("products", http.HandlerFunc(productH.Edit)))
	mux.Handle("POST /products/{id}", gatedWrite("products", http.HandlerFunc(productH.Update)))
	mux.Handle("POST /products/{id}/archive", gatedWrite("products", http.HandlerFunc(productH.Archive)))
	mux.Handle("POST /products/{id}/unarchive", gatedWrite("products", http.HandlerFunc(productH.Unarchive)))
	//
	// Order (outermost to innermost):
	//   Recover -> RequestID -> Logger -> Session -> CSRF -> mux

	// ---- Orders (requires auth + tenant) ----
	mux.Handle("GET /orders", gated("orders", http.HandlerFunc(orderH.Index)))
	mux.Handle("GET /orders/new", gatedWrite("orders", http.HandlerFunc(orderH.New)))
	mux.Handle("POST /orders", gatedWrite("orders", http.HandlerFunc(orderH.Create)))
	mux.Handle("GET /orders/{id}", gated("orders", http.HandlerFunc(orderH.Show)))
	mux.Handle("GET /orders/{id}/edit", gatedWrite("orders", http.HandlerFunc(orderH.Edit)))
	mux.Handle("POST /orders/{id}", gatedWrite("orders", http.HandlerFunc(orderH.Update)))
	mux.Handle("POST /orders/{id}/status", gatedWrite("orders", http.HandlerFunc(orderH.ChangeStatus)))
	mux.Handle("GET /measurements/fields", gated("orders", http.HandlerFunc(orderH.MeasurementFields)))
	mux.Handle("GET /orders/{id}/receipt", gated("orders", http.HandlerFunc(orderH.Receipt)))
	mux.Handle("POST /orders/{id}/public-token/regenerate", gatedWrite("orders", http.HandlerFunc(orderH.RegenerateToken)))
	//
	// ---- Payments ----
	mux.Handle("POST /orders/{id}/payments", gatedWrite("orders", http.HandlerFunc(paymentH.Record)))
	mux.Handle("POST /payments/{id}/attachments", gatedWrite("orders", http.HandlerFunc(attachH.UploadToPayment)))
	mux.Handle("POST /payments/{id}/reverse", gatedWrite("orders", http.HandlerFunc(paymentH.Reverse)))

	mux.Handle("POST /orders/{id}/costs", gatedWrite("orders", http.HandlerFunc(costH.Add)))
	mux.Handle("POST /costs/{id}", gatedWrite("orders", http.HandlerFunc(costH.Update)))
	mux.Handle("POST /costs/{id}/delete", gatedWrite("orders", http.HandlerFunc(costH.Delete)))
	// ---- Aso-ebi groups (requires auth + tenant + tailoring trade) ----
	// Tailoring-only: hidden from other business types entirely.
	tailored := func(h http.Handler) http.Handler {
		return middleware.RequireTenant(middleware.RequireTailoring(
			middleware.RequireFlag(flagProvider, "orders")(h)))
	}
	// tailoredWrite adds role authorization for group mutations.
	tailoredWrite := func(h http.Handler) http.Handler {
		return middleware.RequireTenant(middleware.RequireWrite(middleware.RequireTailoring(
			middleware.RequireFlag(flagProvider, "orders")(h))))
	}
	mux.Handle("GET /groups", tailored(http.HandlerFunc(groupH.Index)))
	mux.Handle("GET /groups/new", tailoredWrite(http.HandlerFunc(groupH.New)))
	mux.Handle("POST /groups", tailoredWrite(http.HandlerFunc(groupH.Create)))
	mux.Handle("GET /groups/{id}", tailored(http.HandlerFunc(groupH.Show)))
	mux.Handle("POST /groups/{id}/orders", tailoredWrite(http.HandlerFunc(groupH.AddOrder)))
	mux.Handle("POST /groups/{id}/orders/{orderID}/remove", tailoredWrite(http.HandlerFunc(groupH.RemoveOrder)))
	mux.Handle("POST /groups/{id}/orders/{orderID}/paid", tailoredWrite(http.HandlerFunc(groupH.SetPaid)))
	mux.Handle("POST /groups/{id}/orders/{orderID}/collected", tailoredWrite(http.HandlerFunc(groupH.SetCollected)))
	mux.Handle("POST /groups/{id}/join-toggle", tailoredWrite(http.HandlerFunc(groupH.ToggleJoin)))
	mux.Handle("POST /groups/{id}/delete", tailoredWrite(http.HandlerFunc(groupH.Delete)))
	// ---- Public Aso-ebi join + bride manage (no login) ----
	// Join slug is public (WhatsApp groups); bride manage needs ?key=.
	// Rate-limited like other public POSTs (abuse guard).
	mux.Handle("GET /g/{token}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 60, Window: time.Minute,
		Message: "Too many requests. Please slow down.",
	})(http.HandlerFunc(groupPublicH.JoinForm)))
	mux.Handle("POST /g/{token}", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 10, Window: time.Minute,
		Message: "Too many submission attempts. Please wait a minute.",
	})(http.HandlerFunc(groupPublicH.JoinSubmit)))
	mux.Handle("GET /g/{token}/manage", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 60, Window: time.Minute,
		Message: "Too many requests. Please slow down.",
	})(http.HandlerFunc(groupPublicH.Manage)))
	mux.Handle("POST /g/{token}/manage/paid", middleware.RateLimit(middleware.RateLimitConfig{
		Limit: 30, Window: time.Minute,
		Message: "Too many attempts. Please wait a minute.",
	})(http.HandlerFunc(groupPublicH.ManagePaid)))
	// ---- Occasion calendar ----
	mux.Handle("GET /calendar", gated("orders", http.HandlerFunc(calendarH.Index)))
	// ---- Attachments ----
	mux.Handle("POST /orders/{id}/attachments", gatedWrite("orders", http.HandlerFunc(attachH.UploadToOrder)))
	mux.Handle("GET /attachments/{id}", gated("orders", http.HandlerFunc(attachH.Download)))
	// Session must run before CSRF (CSRF does not need it but templates do).
	// Session must run before any handler that reads the context.
	handler := chain(mux,
		observe.Traced,
		middleware.SecurityHeaders,
		middleware.Recover(renderer),
		middleware.RequestID,
		middleware.Logger,
		middleware.ImpersonationMiddleware(adminSvc),
		middleware.SessionMiddleware(
			authService,
			cfg.Session.CookieName,
			adminSvc,
			buildImpersonatedSession,
		),
		middleware.CSRF(middleware.CSRFConfig{
			CookieName: cfg.Session.CSRFCookieName,
			Secure:     cfg.SecureCookies(),
		}),
		middleware.NotFoundInterceptor(renderer),
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
	// Fan-out to Sentry Logs when enabled (stdout output unchanged).
	slog.SetDefault(slog.New(observe.SentryHandler(h)))
}
