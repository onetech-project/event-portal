/**
 * Every knob the UAT run needs, in one place, with defaults that match a stock
 * `docker compose up` plus the two dev servers.
 *
 * Nothing here is a secret: this suite only ever points at a disposable local
 * stack, and it TRUNCATEs the database it connects to. That is also why
 * DATABASE_URL has no production-shaped default — see assertDisposableDatabase.
 */

/**
 * Which stack this run drives.
 *
 *   local (default) — the disposable stack this file's defaults describe. The
 *                     suite owns it: it starts the servers and TRUNCATEs the
 *                     database between tests.
 *   uat             — a deployed environment. The suite owns nothing there: no
 *                     database, no server lifecycle, and data it creates lives
 *                     alongside real data. See uat/ and support/uat.ts.
 *
 * The two profiles read different files precisely so that pointing one at the
 * other's stack takes a deliberate act rather than an edited line.
 */
export type Target = "local" | "uat";

const target = (process.env.E2E_TARGET ?? "local").trim().toLowerCase();
if (target !== "local" && target !== "uat") {
  throw new Error(`E2E_TARGET must be "local" or "uat", got "${target}".`);
}
const remote = target === "uat";

/**
 * An optional env file fills in whatever the shell did not set. Node's own
 * loader leaves existing process.env entries alone, so precedence is
 * shell > file > the defaults below — `E2E_SLOW_MO=500 playwright test` still
 * wins over a file that says otherwise.
 *
 * Resolved against this file rather than the cwd so it is found no matter where
 * the runner was invoked from. Absent file is not an error for the local
 * profile, whose defaults already describe a stock local stack; the uat profile
 * has no defaults to fall back on and fails on the first missing value instead.
 */
try {
  process.loadEnvFile(new URL(remote ? "../.env.uat" : "../.env", import.meta.url));
} catch {
  // No file — the local defaults stand, and `required` speaks for the uat profile.
}

function env(name: string, fallback: string): string {
  const value = process.env[name];
  return value === undefined || value === "" ? fallback : value;
}

/**
 * A value the uat profile cannot invent. There is no sensible default for
 * "which deployment" or "what is its callback token", and a wrong guess would
 * point a browser at localhost and report the deployment healthy.
 */
function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value.trim() === "") {
    throw new Error(
      `${name} must be set for E2E_TARGET=uat. Copy e2e/.env.uat.example to e2e/.env.uat and fill it in.`,
    );
  }
  return value;
}

/** Required against a deployment, defaulted against the local stack. */
function targeted(name: string, localFallback: string): string {
  return remote ? required(name) : env(name, localFallback);
}

export const config = {
  /** "local" | "uat" — see Target above. */
  target: target as Target,
  /** True when this run drives a deployment it does not own. */
  remote,

  /** Where the browser goes. */
  frontendURL: targeted("E2E_FRONTEND_URL", "http://localhost:3100"),
  /** Where the API answers, including the /api/v1 prefix. */
  apiURL: targeted("E2E_API_URL", "http://localhost:8100/api/v1"),
  /** Same host without the prefix, for /healthz and /metrics. */
  apiRootURL: targeted("E2E_API_ROOT_URL", "http://localhost:8100"),
  /** The Manjo gateway stub this run drives payment through. */
  gatewayStubURL: env("E2E_GATEWAY_URL", "http://localhost:8101"),

  /**
   * Empty on the uat profile, and deliberately not overridable there: the suite
   * has no database access to a deployment, and support/db.ts refuses to open a
   * pool at all. Every uat assertion goes through the API.
   */
  databaseURL: remote
    ? ""
    : env(
        "E2E_DATABASE_URL",
        "postgres://ticketing:ticketing@localhost:5433/ticketing?sslmode=disable",
      ),
  redisURL: remote ? "" : env("E2E_REDIS_URL", "redis://localhost:6380/0"),

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
   *
   * Against a deployment this is a real secret and has no default: it must be
   * the deployment's own PG_CALLBACK_TOKEN, or settlement cannot be driven at
   * all. Locally it is a fixture value that playwright.config.ts hands to the
   * API it starts, so both sides agree by construction.
   */
  pgCallbackToken: targeted("E2E_PG_CALLBACK_TOKEN", "uat-e2e-callback-token"),

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
    email: targeted("E2E_ADMIN_EMAIL", "uat@example.com"),
    password: targeted("E2E_ADMIN_PASSWORD", "uat-password-2026"),
    /**
     * bcrypt of the password above, generated once with golang.org/x/crypto/bcrypt
     * at the default cost. Precomputed so seeding needs no bcrypt dependency and
     * no Go toolchain at test time. Regenerate if the password changes.
     *
     * Local only: it exists to seed the admin row. A deployment's admin already
     * exists and is signed in to, never created, so the uat profile leaves this
     * empty rather than inviting someone to paste a hash it would never use.
     */
    passwordHash: remote
      ? ""
      : env(
          "E2E_ADMIN_PASSWORD_HASH",
          "$2a$10$CgCyUAfad.aCAZ4LTyZL1..x1ptoMhQ3GpEMnyy.BKDzV/aFMYVpu",
        ),
  },

  /** Whether this run expects the Redis cache to be on. */
  cacheEnabled: env("E2E_CACHE_ENABLED", "true") === "true",

  /**
   * Whether this run expects request throttling to be on (Constitution
   * Principle IX). The suite MUST pass either way; scenarios whose entire
   * subject is a throttle refusal skip themselves when this is false, exactly
   * as the cache-refresh specs skip when the cache is off.
   */
  rateLimitEnabled: env("E2E_RATE_LIMIT_ENABLED", "true") === "true",

  /**
   * Port of the second API instance, which runs with deliberately tiny
   * thresholds so throttle scenarios can trip a limit in a second or two.
   *
   * It exists because throttle state is keyed per client and lives in one
   * process's memory, and every request in a local run arrives from the same
   * address. Draining a bucket on the main API would refuse unrelated
   * scenarios for as long as it took to refill — flakiness injected straight
   * into the acceptance gate.
   */
  throttleApiURL: env("E2E_THROTTLE_API_URL", "http://127.0.0.1:8102"),

  /**
   * Milliseconds to pause before every browser operation, so a headed run is
   * watchable at human speed. 0 (the default) is full speed; the timeouts in
   * playwright.config.ts scale with this so a slow run does not time out.
   */
  slowMo: Math.max(0, Number(env("E2E_SLOW_MO", "0")) || 0),

  /**
   * Set when the suite starts the servers itself (the default locally).
   *
   * Forced off against a deployment: those servers are already running and are
   * not this runner's to own.
   */
  manageServers: !remote && env("E2E_MANAGE_SERVERS", "true") === "true",

  /**
   * Prefix for every event, ticket type and buyer this run creates on a
   * deployment, so its residue is identifiable at a glance and greppable later.
   * Only the uat profile uses it — locally the database is emptied instead.
   */
  uatPrefix: env("E2E_UAT_PREFIX", "uat-e2e"),
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
