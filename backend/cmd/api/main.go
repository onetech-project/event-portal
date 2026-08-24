// Command api is the Event Ticketing HTTP server: the single deployable of the
// modular monolith. It owns process startup, dependency wiring, and routing —
// deliberately the only place where domains are connected to one another.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
)

// rateLimit returns the per-IP limiter for a surface, or a pass-through when it
// is switched off.
//
// The group is constructed either way by the caller — see httpx.PassThrough for
// why a disabled surface must not simply lose its group.
func rateLimit(active bool, requestsPerSecond float64, burst int, idleFor time.Duration) echo.MiddlewareFunc {
	if !active {
		return httpx.PassThrough()
	}
	return httpx.RateLimitPerIP(requestsPerSecond, burst, idleFor)
}

// reportThrottleConfig writes the throttle configuration actually in force to
// the log at startup, so a deployment that believes it changed a limit can
// confirm that it did (spec 018 FR-017).
//
// Deliberately NOT exposed on /healthz or any other endpoint: that route is
// public and unauthenticated, and publishing exact thresholds converts a limit
// into a documented allowance for anyone who asks.
func reportThrottleConfig(log *logger.Logger, cfg *config.Config) {
	t := cfg.Throttle

	if !t.Enabled {
		log.Warn("request throttling is DISABLED for every surface; " +
			"booking holds quota, checkout opens paid gateway sessions, the guest resend " +
			"sends real mail, and held-open status connections are unbounded — all unauthenticated")
		return
	}

	surface := func(p config.ThrottlePolicy) string {
		if !p.Active(t.Enabled) {
			return "off"
		}
		return fmt.Sprintf("%g/s burst %d", p.Rate, p.Burst)
	}
	resend := "off"
	if t.Resend.Active(t.Enabled) {
		resend = fmt.Sprintf("1 per %s burst %d", t.Resend.Window, t.Resend.Burst)
	}
	stream := "off"
	if t.StatusStream.Active(t.Enabled) {
		stream = fmt.Sprintf("%d concurrent", t.StatusStream.MaxConns)
	}

	log.Info("request throttling configured",
		"idle_retention", t.IdleTTL.String(),
		"book", surface(t.Book),
		"availability", surface(t.Availability),
		"ticket_lookup", surface(t.TicketLookup),
		"checkout", surface(t.Checkout),
		"resend", resend,
		"status_stream", stream,
		"client_ip", clientIPSource(cfg))

	for _, a := range cfg.DeprecatedAliases {
		log.Warn("deprecated configuration name in use; it still works but will not forever",
			"using", a.Old, "prefer", a.New)
	}
}

func clientIPSource(cfg *config.Config) string {
	if len(cfg.TrustedProxyCIDRs) == 0 {
		return "connection (X-Forwarded-For ignored)"
	}
	return "X-Forwarded-For from " + strings.Join(cfg.TrustedProxyCIDRs, ",")
}

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

	// checkoutGateway serves two of this package's contracts: the gateway itself,
	// and the read-back of what a started payment was recorded under (spec 017).
	// Its `payments` back-reference is assigned below, once the payment service
	// exists — the same loop, for the same reason.
	orderSvc := order.NewService(pool, orderRepo,
		eventProviderAdapter{events: eventSvc},
		checkoutGateway,
		log).WithPaymentRecords(checkoutGateway).WithCache(listCache).WithTimers(order.Timers{
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

	// Free registration's post-commit work (spec 022). The SAME two values the
	// payment webhook uses, satisfying order's own identically-shaped interfaces
	// — a domain may not import another domain's interfaces, so both declare
	// their own and this file, the one place allowed to see every domain,
	// satisfies both from one ticket service and one deliverer.
	//
	// Wired here rather than at construction because orderSvc is built before
	// ticketSvc and notificationSvc exist. Same loop, same reason, as the two
	// back-references above.
	orderSvc.WithFulfillment(
		ticketSvc, // order.TicketIssuer
		ticketDelivererAdapter{notifications: notificationSvc}, // order.TicketDeliverer
	)

	adminSvc := admin.NewService(adminRepo, admin.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL), log)

	// --- HTTP -------------------------------------------------------------

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = httpx.ErrorHandler(log)

	// Where the client address comes from, and therefore what every per-client
	// throttle is keyed on (Constitution Principle IX).
	//
	// Echo's default, with no extractor installed, returns the first
	// caller-supplied X-Forwarded-For value — so a client rotating that header
	// buys a fresh bucket per request and every per-IP limit below is
	// decorative. Taking the address from the connection is the only safe
	// default; a deployment genuinely behind a proxy names it in
	// TRUSTED_PROXY_CIDRS and gets header-based identification back.
	if len(cfg.TrustedProxyCIDRs) == 0 {
		e.IPExtractor = echo.ExtractIPDirect()
	} else {
		trust := make([]echo.TrustOption, 0, len(cfg.TrustedProxyCIDRs))
		for _, cidr := range cfg.TrustedProxyCIDRs {
			_, netw, err := net.ParseCIDR(cidr)
			if err != nil {
				return err // config validation already rejected this; belt and braces
			}
			trust = append(trust, echo.TrustIPRange(netw))
		}
		e.IPExtractor = echo.ExtractIPFromXFFHeader(trust...)
		log.Info("trusting X-Forwarded-For from configured proxies", "cidrs", cfg.TrustedProxyCIDRs)
	}

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
	paymentHandler := payment.NewHandler(paymentSvc, log).WithStreamCap(
		cfg.Throttle.StatusStream.Active(cfg.Throttle.Enabled),
		cfg.Throttle.StatusStream.MaxConns)
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
		rateLimit(cfg.Throttle.Book.Active(cfg.Throttle.Enabled),
			cfg.Throttle.Book.Rate, cfg.Throttle.Book.Burst, cfg.Throttle.IdleTTL))
	orderHandler.RegisterBookRoute(bookGroup)

	// The availability check in front of the Terms & Conditions gate (spec 013).
	// Its own group and its own budget — see availabilityRate.
	availabilityGroup := e.Group("/api/v1",
		rateLimit(cfg.Throttle.Availability.Active(cfg.Throttle.Enabled),
			cfg.Throttle.Availability.Rate, cfg.Throttle.Availability.Burst, cfg.Throttle.IdleTTL))
	orderHandler.RegisterAvailabilityRoute(availabilityGroup)

	// Free registration (spec 022). The submit deducts quota AND sends real mail
	// on an unauthenticated surface, so it gets its own per-IP budget rather
	// than sharing booking's — a registrant and a buyer should not be able to
	// throttle each other out.
	//
	// The group is constructed in BOTH modes: `rateLimit` substitutes a
	// pass-through when the switch is off rather than being skipped, so an
	// unmatched /api/v1 path answers identically either way (Principle IX).
	registerGroup := e.Group("/api/v1",
		rateLimit(cfg.Throttle.Register.Active(cfg.Throttle.Enabled),
			cfg.Throttle.Register.Rate, cfg.Throttle.Register.Burst, cfg.Throttle.IdleTTL))
	orderHandler.RegisterRegistrationRoute(registerGroup)
	// The form's prerequisites read rides the unthrottled guest group: it creates
	// nothing and sends nothing, and throttling it would refuse a guest reloading
	// the page they are trying to reach.
	orderHandler.RegisterRegistrationReadRoute(api)

	// The public ticket lookup is rate limited per IP so ticket-code enumeration
	// is impractical (spec FR-020).
	ticketLookup := e.Group("/api/v1",
		rateLimit(cfg.Throttle.TicketLookup.Active(cfg.Throttle.Enabled),
			cfg.Throttle.TicketLookup.Rate, cfg.Throttle.TicketLookup.Burst, cfg.Throttle.IdleTTL))
	ticket.NewHandler(ticketSvc).RegisterPublicRoutes(ticketLookup)

	// Checkout opens a gateway session per call, so it sits behind a per-IP limit
	// rather than on the unthrottled read group.
	//
	// The group survives the withdrawal of the status-refresh and QR-refresh
	// routes because checkout still belongs in it — it was never empty of
	// outbound-cost endpoints.
	gatewayCalls := e.Group("/api/v1",
		rateLimit(cfg.Throttle.Checkout.Active(cfg.Throttle.Enabled),
			cfg.Throttle.Checkout.Rate, cfg.Throttle.Checkout.Burst, cfg.Throttle.IdleTTL))
	orderHandler.RegisterCheckoutRoutes(gatewayCalls)

	// The guest resend on the confirmation screen. Limited per order number, which
	// the handler reads from the body and hands to the cooldown itself — no
	// middleware and so no separate group, because the limit now has to answer
	// "how long?" as well as "may I?" (spec 012 FR-021j).
	//
	// rateLimitWindow doubles as the cooldown's idle-eviction period here, and it
	// must stay longer than the window itself: evicting a key mid-cooldown would
	// forgive it silently. Three minutes against sixty seconds.
	resendCooldown := httpx.NewDisabledCooldown()
	if cfg.Throttle.Resend.Active(cfg.Throttle.Enabled) {
		resendCooldown = httpx.NewCooldown(
			cfg.Throttle.Resend.Rate(), cfg.Throttle.Resend.Burst, cfg.Throttle.IdleTTL)
	}
	notification.NewHandler(notificationSvc).RegisterPublicRoutes(api, resendCooldown)

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

	reportThrottleConfig(log, cfg)

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

	// Free registration fulfils off the request path too, and its goroutine is
	// tracked separately because it is dispatched by a different domain. A
	// registration accepted moments before SIGTERM must still issue and deliver.
	log.Info("waiting for in-flight registration fulfilment")
	orderSvc.WaitForRegistrationFulfillment()

	// Flush buffered spans last: shutting the exporter down earlier would drop
	// the traces for the requests just drained.
	if err := shutdownTracing(shutdownCtx); err != nil {
		log.Error("could not flush traces on shutdown", "error", err.Error())
	}

	log.Info("shutdown complete")
	return nil
}
