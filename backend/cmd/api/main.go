// Command api is the Event Ticketing HTTP server: the single deployable of the
// modular monolith. It owns process startup, dependency wiring, and routing —
// deliberately the only place where domains are connected to one another.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"

	"github.com/manjo/ticketing/backend/internal/admin"
	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/config"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/observability"
)

const (
	startupTimeout  = 15 * time.Second
	shutdownTimeout = 30 * time.Second
	// rateLimitWindow is how long an idle per-IP bucket is retained.
	rateLimitWindow = 3 * time.Minute

	// Checkout costs an outbound gateway call per press, so it is limited far more
	// tightly than a read: roughly one every five seconds, with a small burst for
	// an impatient double-tap.
	gatewayCallRate  = 0.2
	gatewayCallBurst = 3

	// The guest resend sends real mail without any authentication, so it is
	// limited per order — one a minute, no burst. Keying on the order rather than
	// the caller is the point: the inbox being protected is the buyer's, and a
	// per-IP limit would let a few hosts flood one buyer between them. The window
	// is also what the confirmation screen counts down from, so changing it
	// changes what a guest is told, not only what they are allowed.
	guestResendRate  = 1.0 / 60.0
	guestResendBurst = 1

	// Booking creates rows and holds quota for an hour without payment, so it is
	// limited per IP: roughly one booking every three seconds with a small burst
	// for a genuine group organizing itself, while a scripted hoarder starves.
	bookRate  = 0.33
	bookBurst = 5

	// The availability check reads and creates nothing, so it is limited far
	// more loosely than booking. It deliberately does NOT share bookRate: the
	// intended flow is check-then-book, plus another check every time a refused
	// guest adjusts their selection and tries again, so charging those to the
	// booking budget would throttle a guest out of the recovery path the check
	// exists to offer. Still limited, because it is unauthenticated and hits the
	// database on every call.
	availabilityRate  = 1.0
	availabilityBurst = 10
)

func main() {
	log := logger.New(logger.ParseLevel(os.Getenv("LOG_LEVEL")))

	if err := run(log); err != nil {
		log.Error("server exited with an error", "error", err.Error())
		os.Exit(1)
	}
}

func run(log *logger.Logger) error {
	// Local convenience only: real environment variables always win, and a
	// missing file is the normal case in a container.
	if err := config.LoadDotEnv(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Installed before the pool and the router so their instrumentation has a
	// provider to report to. With no OTLP endpoint configured this is a no-op and
	// the service behaves exactly as before.
	shutdownTracing, err := observability.InitTracing(context.Background(), observability.TracingConfig{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.ServiceVersion,
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.OTLPEndpoint,
		SampleRatio:    cfg.TraceSampleRatio,
	})
	if err != nil {
		return err
	}
	if cfg.OTLPEndpoint != "" {
		log.Info("exporting traces", "endpoint", cfg.OTLPEndpoint, "sample_ratio", cfg.TraceSampleRatio)
	}

	// Service identity on every log line, so Loki can filter by service and
	// environment without relying on container labels alone.
	log = log.With(
		"service_name", cfg.ServiceName,
		"service_version", cfg.ServiceVersion,
		"environment", cfg.Environment,
	)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	pool, err := db.NewPool(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("connected to the database")

	// --- Cache ------------------------------------------------------------
	//
	// A read cache in front of the list endpoints, and nothing more (Constitution
	// Principle VII). It is a SOFT dependency by design: a dial failure here logs
	// and continues, because an API that refuses to start because a cache is down
	// has turned an accelerator into a liability.
	//
	// cacheRegistry is the same registry the HTTP metrics use when metrics are on,
	// so cache counters appear on the existing /metrics endpoint rather than a
	// second one.
	var listCache cache.Lists = cache.NoOp{}
	var metricsRegistry *prometheus.Registry
	if cfg.MetricsEnabled {
		metricsRegistry = prometheus.NewRegistry()
	}
	if cfg.CacheActive() {
		// A nil registry is fine — NewMetrics skips registration and the counters
		// become cheap no-ops, which is exactly right when metrics are off.
		var reg prometheus.Registerer
		if metricsRegistry != nil {
			reg = metricsRegistry
		}
		rc, err := cache.NewRedis(cfg.RedisURL, cfg.CacheTTL, log, reg)
		if err != nil {
			// A malformed URL is a configuration error, not a runtime one — but it
			// still must not stop the API from serving.
			log.Error("cache disabled: REDIS_URL could not be parsed", "error", err.Error())
		} else {
			defer func() { _ = rc.Close() }()
			listCache = rc
			pingCtx, cancelPing := context.WithTimeout(startupCtx, 2*time.Second)
			if err := rc.Ping(pingCtx); err != nil {
				log.Warn("cache is not reachable at startup; serving from the database until it appears",
					"error", err.Error())
			} else {
				log.Info("connected to the cache", "ttl", cfg.CacheTTL.String())
			}
			cancelPing()
		}
	} else {
		log.Info("cache disabled by configuration; every list read goes to the database")
	}

	// --- Repositories -----------------------------------------------------

	adminRepo := admin.NewRepository(pool)
	eventRepo := event.NewRepository(pool)
	orderRepo := order.NewRepository(pool)
	paymentRepo := payment.NewRepository(pool)
	ticketRepo := ticket.NewRepository(pool)

	// --- Gateway ----------------------------------------------------------

	// There is no sandbox/production selection and no compiled-in hostname: the
	// address comes from configuration or the process does not start (FR-026).
	// Nothing here can recognise a *stale* address, which is a deliberate,
	// recorded trade — catching that belongs to deployment tooling.
	gateway := payment.NewManjoGateway(payment.ManjoConfig{
		BaseURL:        cfg.PGBaseURL,
		ServerKey:      cfg.PGServerKey,
		ClientKey:      cfg.PGClientKey,
		CallbackToken:  cfg.PGCallbackToken,
		ExpectedWindow: cfg.PaymentWindow,
		HTTPClient: &http.Client{
			// Well inside a guest's patience, and short enough that two retries on
			// top of it still fit inside one (FR-007c).
			Timeout: 8 * time.Second,
		},
		Log: log,
	})
	log.Info("payment gateway configured", "provider", gateway.Name(), "base_url", cfg.PGBaseURL)

	// --- Services, wired through the adapters in adapters.go --------------
	//
	// orderRepo satisfies event.OrderChecker structurally, so the event domain
	// gets its delete guards and sold counts without importing internal/order.

	eventSvc := event.NewService(pool, eventRepo, orderRepo, log).WithCache(listCache)

	// Held as a variable because it needs the payment service back-wired into it
	// once that exists — see the comment on gatewayAdapter.payments.
	checkoutGateway := &gatewayAdapter{gateway: gateway, log: log}

	orderSvc := order.NewService(pool, orderRepo,
		eventProviderAdapter{events: eventSvc},
		checkoutGateway,
		log).WithCache(listCache).WithTimers(order.Timers{
		BookingHold:   cfg.BookingHold,
		PaymentWindow: cfg.PaymentWindow,
	})

	adminOrderSvc := order.NewAdminService(orderRepo, orderEventLookupAdapter{events: eventSvc}).WithCache(listCache)

	// The guest's own order page: read-only, unauthenticated, keyed by order
	// number.
	publicOrderSvc := order.NewPublicService(orderRepo, orderEventLookupAdapter{events: eventSvc})

	ticketSvc := ticket.NewService(pool, ticketRepo, orderRepo, log)

	// A pointer, because payments is wired after the payment service exists —
	// the same loop checkoutGateway closes below, for the same reason: the two
	// services are mutually dependent and this file is where that is allowed.
	deliveryOrders := &notificationOrderAdapter{orders: orderRepo, guestReads: publicOrderSvc}

	notificationSvc := notification.NewService(
		deliveryOrders,
		notificationTicketAdapter{tickets: ticketRepo},
		notification.NewSMTPMailer(notification.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			FromName: cfg.SMTPFromName,
		}),
		// Platform-wide branding for the email and both attachments (spec 016
		// FR-035). Identical on every order — the event's own name, venue and
		// address are the only things that vary, and those ride on the order.
		notification.Branding{
			SiteName:     cfg.BrandSiteName,
			SiteURL:      cfg.BrandSiteURL,
			SupportEmail: cfg.BrandSupportEmail,
			LegalEntity:  cfg.BrandLegalEntity,
			Attribution:  cfg.BrandAttribution,
			Copyright:    cfg.BrandCopyright,
			LogoPath:     cfg.BrandLogoPath,
		},
		log)

	paymentSvc := payment.NewService(pool, paymentRepo, gateway,
		paymentOrderAdapter{orders: orderRepo},
		eventSvc,                               // payment.QuotaRestorer
		quotaReserverAdapter{events: eventSvc}, // payment.QuotaReserver — the settle path takes seats back
		paymentOrderAdapter{orders: orderRepo}, // payment.OrderReleaser — EXPIRED → PAID and nothing else
		ticketSvc,                              // payment.TicketIssuer
		ticketDelivererAdapter{notifications: notificationSvc}, // payment.TicketDeliverer (narrows the spec-011 recipient list)
		log).WithCache(listCache, eventSvc) // eventSvc satisfies payment.EventScopeLookup

	// Closes the loop: a checkout whose session-open is refused as a duplicate
	// reference releases its seats and records why, on the order's own history.
	checkoutGateway.payments = paymentSvc

	// And the other loop: the receipt names the instrument the guest actually
	// paid with and when it settled, both of which live on the payments table.
	deliveryOrders.payments = paymentSvc

	adminSvc := admin.NewService(adminRepo, admin.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL), log)

	// --- HTTP -------------------------------------------------------------

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = httpx.ErrorHandler(log)

	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())

	// Tracing first, so every later middleware and handler runs inside the
	// request span and its logs carry the trace id.
	e.Use(otelecho.Middleware(cfg.ServiceName))

	if cfg.MetricsEnabled {
		// Same registry the cache counters registered on, so /metrics carries both.
		metrics := observability.NewMetrics(metricsRegistry)
		e.Use(metrics.Middleware())
		e.GET("/metrics", metrics.Handler())
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{cfg.FrontendURL},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	e.GET("/healthz", healthHandler(pool, listCache))

	api := e.Group("/api/v1")

	// Public, unauthenticated guest surface (Constitution Principle VI).
	event.NewHandler(eventSvc).RegisterPublicRoutes(api)
	orderHandler := order.NewHandler(orderSvc, publicOrderSvc, log)
	orderHandler.RegisterPublicRoutes(api)
	paymentHandler := payment.NewHandler(paymentSvc, log)
	// The gateway notification endpoint. Mounted on the Echo instance rather than
	// on `api` because its path is fixed by the gateway's dispatch code
	// (/v1.0/callback/exec) — versioning it under /api/v1 would simply put it
	// somewhere the gateway cannot reach.
	paymentHandler.RegisterCallbackRoute(e)
	// Live checkout status (SSE). It caps concurrent connections per IP itself,
	// so it mounts on the unthrottled group.
	paymentHandler.RegisterStatusStream(api)

	// Booking creates rows and holds quota for an hour, so it sits behind its
	// own per-IP limiter rather than sharing the unthrottled guest group.
	bookGroup := e.Group("/api/v1",
		httpx.RateLimitPerIP(bookRate, bookBurst, rateLimitWindow))
	orderHandler.RegisterBookRoute(bookGroup)

	// The availability check in front of the Terms & Conditions gate (spec 013).
	// Its own group and its own budget — see availabilityRate.
	availabilityGroup := e.Group("/api/v1",
		httpx.RateLimitPerIP(availabilityRate, availabilityBurst, rateLimitWindow))
	orderHandler.RegisterAvailabilityRoute(availabilityGroup)

	// The public ticket lookup is rate limited per IP so ticket-code enumeration
	// is impractical (spec FR-020).
	ticketLookup := e.Group("/api/v1",
		httpx.RateLimitPerIP(cfg.TicketLookupRateLimit, cfg.TicketLookupBurst, rateLimitWindow))
	ticket.NewHandler(ticketSvc).RegisterPublicRoutes(ticketLookup)

	// Checkout opens a gateway session per call, so it sits behind a per-IP limit
	// rather than on the unthrottled read group.
	//
	// The group survives the withdrawal of the status-refresh and QR-refresh
	// routes because checkout still belongs in it — it was never empty of
	// outbound-cost endpoints.
	gatewayCalls := e.Group("/api/v1",
		httpx.RateLimitPerIP(gatewayCallRate, gatewayCallBurst, rateLimitWindow))
	orderHandler.RegisterCheckoutRoutes(gatewayCalls)

	// The guest resend on the confirmation screen. Limited per order number, which
	// the handler reads from the body and hands to the cooldown itself — no
	// middleware and so no separate group, because the limit now has to answer
	// "how long?" as well as "may I?" (spec 012 FR-021j).
	//
	// rateLimitWindow doubles as the cooldown's idle-eviction period here, and it
	// must stay longer than the window itself: evicting a key mid-cooldown would
	// forgive it silently. Three minutes against sixty seconds.
	notification.NewHandler(notificationSvc).RegisterPublicRoutes(api,
		httpx.NewCooldown(guestResendRate, guestResendBurst, rateLimitWindow))

	// Abandoned orders release their seats without anyone opening the page.
	sweeper := payment.NewSweeper(paymentSvc, cfg.PaymentSweepInterval)
	sweepCtx, stopSweeper := context.WithCancel(context.Background())
	defer stopSweeper()
	go sweeper.Run(sweepCtx)
	log.Info("payment expiry sweeper started", "interval", cfg.PaymentSweepInterval.String())

	// Login is the one /admin/* route reachable without a token.
	admin.NewHandler(adminSvc).RegisterRoutes(api)

	// Everything else under /admin/* is JWT protected.
	adminAPI := e.Group("/api/v1", admin.RequireAuth(admin.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL)))
	eventAdminHandler := event.NewHandler(eventSvc)
	eventAdminHandler.RegisterAdminRoutes(adminAPI)
	// CMS content surface (spec 008 US4): terms + content blocks, same JWT group.
	eventAdminHandler.RegisterAdminContentRoutes(adminAPI)
	event.NewHandler(eventSvc).RegisterPackageRoutes(adminAPI)
	order.NewAdminHandler(adminOrderSvc).RegisterAdminRoutes(adminAPI)
	ticket.NewHandler(ticketSvc).RegisterAdminRoutes(adminAPI)
	notification.NewHandler(notificationSvc).RegisterAdminRoutes(adminAPI)
	// The two order-scoped read views (spec 012 US6). Reads only: a payment whose
	// notification was lost is recovered by asking the gateway to resend it, and
	// these are what an operator checks first — the order's notification history,
	// and what it holds against what remains.
	paymentHandler.RegisterAdminRoutes(adminAPI)

	adminAPI.POST("/admin/cache/refresh", cacheRefreshHandler(listCache, log))

	// --- Serve, then drain ------------------------------------------------

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "port", cfg.AppPort)
		if err := e.Start(":" + cfg.AppPort); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case sig := <-stop:
		log.Info("shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return err
	}

	// Stop sweeping before draining: a sweep starting now would only be cut off
	// mid-transaction.
	stopSweeper()

	// Post-payment work runs off the request path, so draining the HTTP server is
	// not enough: wait for any in-flight ticket generation and email delivery
	// before the process exits.
	log.Info("waiting for in-flight post-payment work")
	paymentSvc.WaitForFulfillment()

	// Flush buffered spans last: shutting the exporter down earlier would drop
	// the traces for the requests just drained.
	if err := shutdownTracing(shutdownCtx); err != nil {
		log.Error("could not flush traces on shutdown", "error", err.Error())
	}

	log.Info("shutdown complete")
	return nil
}
