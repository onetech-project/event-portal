import { apiBaseUrl } from "./env";
import { clearToken, readToken } from "./auth";

/**
 * An error carrying the API's machine-readable error_code, so callers can branch
 * on the contract rather than on message text.
 *
 * status is 0 for a transport failure, where the server never answered.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

type RequestOptions = {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  headers?: Record<string, string>;
};

/**
 * Calls the API and returns the parsed body.
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
      "NETWORK_ERROR",
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
  return JSON.parse(text) as T;
}

/** Calls an authenticated admin endpoint, attaching the stored bearer token. */
export async function adminFetch<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const token = readToken();
  if (!token) {
    throw new ApiError(401, "UNAUTHORIZED", "Your session has expired. Please sign in again.");
  }

  try {
    return await apiFetch<T>(path, {
      ...options,
      headers: { ...options.headers, Authorization: `Bearer ${token}` },
    });
  } catch (error) {
    // Drop a token the server has rejected, so the next navigation goes to the
    // login screen instead of retrying a credential that will never work.
    if (error instanceof ApiError && error.status === 401) {
      clearToken();
    }
    throw error;
  }
}

async function toApiError(response: Response): Promise<ApiError> {
  const fallback = `Request failed with status ${response.status}.`;

  try {
    const body = (await response.json()) as { error_code?: string; message?: string };
    return new ApiError(
      response.status,
      body.error_code ?? "UNKNOWN_ERROR",
      body.message ?? fallback,
    );
  } catch {
    // A proxy or gateway can return HTML or plain text; the caller still needs a
    // well-formed error.
    return new ApiError(response.status, "UNKNOWN_ERROR", fallback);
  }
}
