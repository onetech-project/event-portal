/**
 * Runtime configuration for the browser bundle.
 *
 * Read through a function rather than a module constant so tests (and any future
 * runtime-injected config) can change it without the value being frozen at import
 * time.
 */
export function apiBaseUrl(): string {
  return (
    process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080/api/v1"
  ).replace(/\/$/, "");
}

/**
 * The API's origin, for the few resources the API serves by absolute path
 * rather than relative to its versioned base — currently the on-demand QR
 * image. Derived from the base URL so there is only one thing to configure.
 */
export function apiOrigin(): string {
  try {
    return new URL(apiBaseUrl()).origin;
  } catch {
    return "";
  }
}
