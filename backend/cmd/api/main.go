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
	"github.com/manjo/ticketing/backend/pkg/apperr"
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

	// --- Repositories -----------------------------------------------------

	adminRepo := admin.NewRepository(pool)
	eventRepo := event.NewRepository(pool)
	orderRepo := order.NewRepository(pool)
	paymentRepo := payment.NewRepository(pool)
	ticketRepo := ticket.NewRepository(pool)

	// --- Gateway ----------------------------------------------------------

	snapBaseURL := payment.SnapSandboxBaseURL
	if cfg.MidtransIsProduction {
		snapBaseURL = payment.SnapProductionBaseURL
	}
	if cfg.MidtransBaseURL != "" {
		snapBaseURL = cfg.MidtransBaseURL
		log.Warn("using an overridden payment gateway endpoint", "base_url", snapBaseURL)
	}
	gateway := payment.NewMidtransGateway(cfg.MidtransServerKey, snapBaseURL, &http.Client{
		Timeout: 15 * time.Second,
	})

	// --- Services, wired through the adapters in adapters.go --------------
	//
	// orderRepo satisfies event.OrderChecker structurally, so the event domain
	// gets its delete guards and sold counts without importing internal/order.

	eventSvc := event.NewService(pool, eventRepo, orderRepo, log)

	orderSvc := order.NewService(pool, orderRepo,
		eventProviderAdapter{events: eventSvc},
		gatewayAdapter{gateway: gateway},
		log)

	adminOrderSvc := order.NewAdminService(orderRepo, orderEventLookupAdapter{events: eventSvc})

	ticketSvc := ticket.NewService(pool, ticketRepo, orderRepo, log)

	notificationSvc := notification.NewService(
		notificationOrderAdapter{orders: orderRepo},
		notificationTicketAdapter{tickets: ticketRepo},
		notification.NewSMTPMailer(notification.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			FromName: cfg.SMTPFromName,
		}),
		log)

	paymentSvc := payment.NewService(pool, paymentRepo, gateway,
		paymentOrderAdapter{orders: orderRepo},
		eventSvc,        // payment.QuotaRestorer
		ticketSvc,       // payment.TicketIssuer
		notificationSvc, // payment.TicketDeliverer
		log)

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
		metrics := observability.NewMetrics(prometheus.NewRegistry())
		e.Use(metrics.Middleware())
		e.GET("/metrics", metrics.Handler())
	}
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{cfg.FrontendURL},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	e.GET("/healthz", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			return apperr.Wrap(err, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE",
				"The database is not reachable.")
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	api := e.Group("/api/v1")

	// Public, unauthenticated guest surface (Constitution Principle VI).
	event.NewHandler(eventSvc).RegisterPublicRoutes(api)
	order.NewHandler(orderSvc, log).RegisterPublicRoutes(api)
	payment.NewHandler(paymentSvc, log).RegisterPublicRoutes(api)

	// The public ticket lookup is rate limited per IP so ticket-code enumeration
	// is impractical (spec FR-020).
	ticketLookup := e.Group("/api/v1",
		httpx.RateLimitPerIP(cfg.TicketLookupRateLimit, cfg.TicketLookupBurst, rateLimitWindow))
	ticket.NewHandler(ticketSvc).RegisterPublicRoutes(ticketLookup)

	// Login is the one /admin/* route reachable without a token.
	admin.NewHandler(adminSvc).RegisterRoutes(api)

	// Everything else under /admin/* is JWT protected.
	adminAPI := e.Group("/api/v1", admin.RequireAuth(admin.NewTokenIssuer(cfg.JWTSecret, cfg.JWTTTL)))
	event.NewHandler(eventSvc).RegisterAdminRoutes(adminAPI)
	order.NewAdminHandler(adminOrderSvc).RegisterAdminRoutes(adminAPI)
	ticket.NewHandler(ticketSvc).RegisterAdminRoutes(adminAPI)
	notification.NewHandler(notificationSvc).RegisterAdminRoutes(adminAPI)

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
