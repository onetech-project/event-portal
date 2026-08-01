const STORAGE_KEY = "ticketing.admin.token";

type StoredToken = {
  token: string;
  expiresAt: string;
};

/**
 * Why a session ended, so the login screen can say something more useful than
 * "please sign in".
 */
export type SessionEndReason = "expired" | "signed-out";

/**
 * The three states an admin session can be in.
 *
 * "loading" is the important one: during the server render and the hydration
 * render there is no localStorage to read, and a guard that treats that absence
 * as "not signed in" bounces the admin to the login screen on every refresh.
 */
export type SessionState =
  | { status: "loading" }
  | { status: "authenticated"; token: string }
  | { status: "anonymous"; reason?: SessionEndReason };

type Listener = () => void;

const listeners = new Set<Listener>();

/** Why the last session ended, remembered until the login screen consumes it. */
let lastEndReason: SessionEndReason | undefined;

/**
 * Subscribes to session changes from either tab.
 *
 * The native `storage` event fires only in *other* tabs, so a sign-out in this
 * one would otherwise leave the current page rendering admin content until the
 * next navigation. The in-memory listener set covers that gap.
 */
export function subscribeToSession(onChange: Listener): () => void {
  listeners.add(onChange);

  const onStorage = (event: StorageEvent) => {
    // key is null when the whole store is cleared.
    if (event.key === null || event.key === STORAGE_KEY) onChange();
  };
  window.addEventListener("storage", onStorage);

  return () => {
    listeners.delete(onChange);
    window.removeEventListener("storage", onStorage);
  };
}

function notify(): void {
  for (const listener of listeners) listener();
}

/** Persists the admin access token and the expiry the server reported. */
export function storeToken(token: string, expiresAt: string): void {
  if (typeof window === "undefined") return;
  const entry: StoredToken = { token, expiresAt };
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(entry));
  lastEndReason = undefined;
  notify();
}

/**
 * Reads the stored session.
 *
 * Callers on a rendering path must not use this before mount — see
 * {@link SessionState} — but it is safe to call at any time: outside a browser
 * it reports "loading" rather than guessing.
 */
export function readSession(): SessionState {
  if (typeof window === "undefined") return { status: "loading" };

  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) return { status: "anonymous", reason: lastEndReason };

  let entry: Partial<StoredToken>;
  try {
    entry = JSON.parse(raw) as Partial<StoredToken>;
  } catch {
    // A hand-edited or truncated entry is not a session; drop it silently
    // rather than show an error page.
    clearToken();
    return { status: "anonymous" };
  }

  if (!entry.token || !entry.expiresAt) {
    clearToken();
    return { status: "anonymous" };
  }

  const expiresAt = Date.parse(entry.expiresAt);
  if (Number.isNaN(expiresAt) || expiresAt <= Date.now()) {
    // Expiry is checked here so an admin lands on the login screen rather than
    // watching every request fail with a 401.
    clearToken("expired");
    return { status: "anonymous", reason: "expired" };
  }

  return { status: "authenticated", token: entry.token };
}

/** Returns the stored token, or null when there is no usable session. */
export function readToken(): string | null {
  const session = readSession();
  return session.status === "authenticated" ? session.token : null;
}

/**
 * Removes the stored token and records why, so the login screen can explain
 * itself. Notifies this tab as well as the others.
 */
export function clearToken(reason: SessionEndReason = "signed-out"): void {
  if (typeof window === "undefined") return;
  lastEndReason = reason;
  window.localStorage.removeItem(STORAGE_KEY);
  notify();
}

/** Reads and consumes why the last session ended. */
export function takeSessionEndReason(): SessionEndReason | undefined {
  const reason = lastEndReason;
  lastEndReason = undefined;
  return reason;
}

/** Reports whether a usable, unexpired token is stored. */
export function isAuthenticated(): boolean {
  return readToken() !== null;
}
