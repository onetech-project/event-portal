const STORAGE_KEY = "ticketing.admin.token";

type StoredToken = {
  token: string;
  expiresAt: string;
};

/** Persists the admin access token and the expiry the server reported. */
export function storeToken(token: string, expiresAt: string): void {
  if (typeof window === "undefined") return;
  const entry: StoredToken = { token, expiresAt };
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(entry));
}

/**
 * Returns the stored token, or null when there is none, it has expired, or the
 * entry is unreadable.
 *
 * Expiry is checked here so an admin lands on the login screen rather than
 * watching every request fail with a 401.
 */
export function readToken(): string | null {
  if (typeof window === "undefined") return null;

  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;

  let entry: Partial<StoredToken>;
  try {
    entry = JSON.parse(raw) as Partial<StoredToken>;
  } catch {
    clearToken();
    return null;
  }

  if (!entry.token || !entry.expiresAt) {
    clearToken();
    return null;
  }

  const expiresAt = Date.parse(entry.expiresAt);
  if (Number.isNaN(expiresAt) || expiresAt <= Date.now()) {
    clearToken();
    return null;
  }

  return entry.token;
}

/** Removes the stored token. */
export function clearToken(): void {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(STORAGE_KEY);
}

/** Reports whether a usable, unexpired token is stored. */
export function isAuthenticated(): boolean {
  return readToken() !== null;
}
