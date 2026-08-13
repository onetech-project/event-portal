/**
 * Every knob the UAT run needs, in one place, with defaults that match a stock
 * `docker compose up` plus the two dev servers.
 *
 * Nothing here is a secret: this suite only ever points at a disposable local
 * stack, and it TRUNCATEs the database it connects to. That is also why
 * DATABASE_URL has no production-shaped default — see assertDisposableDatabase.
 */

function env(name: string, fallback: string): string {
  const value = process.env[name];
  return value === undefined || value === "" ? fallback : value;
}

export const config = {
  /** Where the browser goes. */
  frontendURL: env("E2E_FRONTEND_URL", "http://localhost:3100"),
  /** Where the API answers, including the /api/v1 prefix. */
  apiURL: env("E2E_API_URL", "http://localhost:8100/api/v1"),
  /** Same host without the prefix, for /healthz and /metrics. */
  apiRootURL: env("E2E_API_ROOT_URL", "http://localhost:8100"),
  /** The Manjo gateway stub this run drives payment through. */
  gatewayStubURL: env("E2E_GATEWAY_URL", "http://localhost:8101"),

  databaseURL: env(
    "E2E_DATABASE_URL",
    "postgres://ticketing:ticketing@localhost:5433/ticketing?sslmode=disable",
  ),
  redisURL: env("E2E_REDIS_URL", "redis://localhost:6380/0"),

  /**
   * Mailpit's REST API, where the delivered ticket email is read back.
   *
   * The suite used to assert delivery only through orders.email_sent, which says
   * a send succeeded and nothing about what was sent. "Exactly two attachments"
   * (spec 016 FR-001) is not observable that way, so the specs that assert on the
   * message itself require Mailpit to actually be up — see mail.ts.
   */
  mailpitURL: env("E2E_MAILPIT_URL", "http://localhost:8025"),

  /**
   * The bearer token inbound notifications must present, which must match the
   * API's PG_CALLBACK_TOKEN.
   *
   * Manjo notifications carry no signature, no digest, and no field that could
   * authenticate them — the token is the entire mechanism (see VerifyWebhook in
   * `internal/payment/manjo.go`). So this is the whole of what the suite needs
   * to impersonate the gateway, and presenting the wrong one is how the
   * rejection path gets proven.
   */
  pgCallbackToken: env("E2E_PG_CALLBACK_TOKEN", "uat-e2e-callback-token"),

  /**
   * The vestigial key pair. The gateway sends these in the session-open body as
   * `ac.cr.{client_id,client_secret}` and Manjo checks only that they are
   * present, so they grant nothing and the stub does not inspect them. They
   * exist here so the request the stub receives is shaped like the real one.
   */
  pgServerKey: env("E2E_PG_SERVER_KEY", "uat-e2e-server-key"),
  pgClientKey: env("E2E_PG_CLIENT_KEY", "uat-e2e-client-key"),
  jwtSecret: env("E2E_JWT_SECRET", "uat-e2e-jwt-secret-long-enough-for-hs256"),

  admin: {
    email: env("E2E_ADMIN_EMAIL", "uat@example.com"),
    password: env("E2E_ADMIN_PASSWORD", "uat-password-2026"),
    /**
     * bcrypt of the password above, generated once with golang.org/x/crypto/bcrypt
     * at the default cost. Precomputed so seeding needs no bcrypt dependency and
     * no Go toolchain at test time. Regenerate if the password changes.
     */
    passwordHash: env(
      "E2E_ADMIN_PASSWORD_HASH",
      "$2a$10$CgCyUAfad.aCAZ4LTyZL1..x1ptoMhQ3GpEMnyy.BKDzV/aFMYVpu",
    ),
  },

  /** Whether this run expects the Redis cache to be on. */
  cacheEnabled: env("E2E_CACHE_ENABLED", "true") === "true",

  /**
   * Milliseconds to pause before every browser operation, so a headed run is
   * watchable at human speed. 0 (the default) is full speed; the timeouts in
   * playwright.config.ts scale with this so a slow run does not time out.
   */
  slowMo: Math.max(0, Number(env("E2E_SLOW_MO", "0")) || 0),

  /** Set when the suite starts the servers itself (the default). */
  manageServers: env("E2E_MANAGE_SERVERS", "true") === "true",
} as const;

/**
 * Refuses to run against anything that looks like a real database.
 *
 * The suite TRUNCATEs every table on every run. That is the right behaviour for
 * a disposable local stack and a catastrophe anywhere else, so the guard is a
 * hard failure rather than a warning — a UAT suite that can be pointed at
 * production by a stray environment variable is a liability, not a test.
 */
export function assertDisposableDatabase(url: string): void {
  const host = new URL(url.replace(/^postgres(ql)?:/, "http:")).hostname;
  const local = ["localhost", "127.0.0.1", "::1", "postgres", "db", "host.docker.internal"];

  if (!local.includes(host)) {
    throw new Error(
      `Refusing to run: E2E_DATABASE_URL points at "${host}", which is not a local host.\n` +
        `This suite TRUNCATEs every table. Point it at a disposable database.`,
    );
  }
  if (/prod|production|live/i.test(url)) {
    throw new Error(
      `Refusing to run: E2E_DATABASE_URL looks production-shaped (${url.replace(/:[^:@]*@/, ":***@")}).`,
    );
  }
}
