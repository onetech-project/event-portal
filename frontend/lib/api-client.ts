import { apiBaseUrl } from "./env";
import { clearToken, readToken } from "./auth";

/**
 * The one JSON shape every endpoint returns (spec 008, clarification
 * 2026-08-05): success is code 200000 with the payload in data; errors carry a
 * numeric code from the registry in contracts/api.md and optional detail data
 * (a validation field map, the current QR payload on 409004).
 */
type Envelope<T> = {
  code: number;
  message: string;
  data: T;
};

/** Numeric envelope codes callers branch on (registry: specs/008 contracts). */
export const API_CODES = {
  success: 200000,
  validation: 400001,
  insufficientQuota: 400002,
  termsNotAccepted: 400003,
  unauthorized: 401001,
  notFound: 404001,
  termsNotAuthored: 404002,
  termsMissing: 409001,
  // 409007 was EMAIL_ALREADY_REGISTERED. Spec 022 FR-023 removed the rule — an
  // address may register as many times as quota allows — and the server retired
  // the number rather than reassigning it, so nothing is mapped here either.
  termsChanged: 409002,
  termsNotRecorded: 409003,
  paymentAlreadyStarted: 409004,
  paymentNotStarted: 409005,
  orderExpired: 410001,
  rateLimited: 429001,
  internal: 500000,
  paymentInitiationFailed: 502001,
  /** Transport failure — the server never answered. */
  network: 0,
} as const;

/**
 * An error carrying the API's numeric envelope code, so callers branch on the
 * contract rather than on message text. data holds the error's detail payload
 * when the contract attaches one (field map, current QR payload).
 *
 * status is 0 for a transport failure, where the server never answered.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: number;
  readonly data: unknown;

  constructor(status: number, code: number, message: string, data: unknown = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.data = data;
  }
}

type RequestOptions = {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  headers?: Record<string, string>;
};

/**
 * Calls the API and returns the envelope's unwrapped data.
 *
 * GETs are sent with `cache: "no-store"`: event listings and quotas are live
 * inventory, and a cached response would offer a guest tickets that are gone.
 */
export async function apiFetch<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { method = "GET", body, headers = {} } = options;

  const init: RequestInit = {
    method,
    cache: "no-store",
    headers: body === undefined ? headers : { "Content-Type": "application/json", ...headers },
  };
  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }

  let response: Response;
  try {
    response = await fetch(`${apiBaseUrl()}${path}`, init);
  } catch {
    // A DNS failure or a dropped connection surfaces as a TypeError; wrapping it
    // means callers only ever handle one error shape.
    throw new ApiError(
      0,
      API_CODES.network,
      "Could not reach the server. Check your connection and try again.",
    );
  }

  if (!response.ok) {
    throw await toApiError(response);
  }

  if (response.status === 204 || response.headers.get("Content-Length") === "0") {
    return undefined as T;
  }

  const text = await response.text();
  if (text === "") {
    return undefined as T;
  }
  const envelope = JSON.parse(text) as Envelope<T>;
  return envelope.data;
}

/** Calls an authenticated admin endpoint, attaching the stored bearer token. */
export async function adminFetch<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const token = readToken();
  if (!token) {
    throw new ApiError(401, API_CODES.unauthorized, "Your session has expired. Please sign in again.");
  }

  try {
    return await apiFetch<T>(path, {
      ...options,
      headers: { ...options.headers, Authorization: `Bearer ${token}` },
    });
  } catch (error) {
    // Drop a token the server has rejected, so the next navigation goes to the
    // login screen instead of retrying a credential that will never work. It is
    // recorded as an expiry: from the admin's side a token the server refuses is
    // indistinguishable from one that timed out, and that is the more useful
    // thing to tell them.
    if (error instanceof ApiError && error.status === 401) {
      clearToken("expired");
    }
    throw error;
  }
}

async function toApiError(response: Response): Promise<ApiError> {
  const fallback = `Request failed with status ${response.status}.`;

  try {
    const body = (await response.json()) as Partial<Envelope<unknown>>;
    return new ApiError(
      response.status,
      body.code ?? response.status * 1000,
      body.message ?? fallback,
      body.data ?? null,
    );
  } catch {
    // A proxy or gateway can return HTML or plain text; the caller still needs a
    // well-formed error. status*1000 keeps the numeric-code invariant.
    return new ApiError(response.status, response.status * 1000, fallback);
  }
}
