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

/**
 * The QRIS frame identity shown around the code: the merchant name a guest is
 * told to verify, the merchant registration number, and the terminal label.
 *
 * Configured rather than parsed out of the payload. Everything here IS present
 * in the QRIS payload — the merchant name at tag 59, the registration number at
 * 26→01, the terminal label at 62→07 — so deriving it is available later at low
 * cost and is recorded as deferred work. It is not built now.
 *
 * The consequence is worth stating plainly, because the instructions tell the
 * guest to check the merchant name before entering a PIN: a label configured
 * here that disagrees with the code defeats the very check it invites. Verifying
 * these against a genuinely issued code is a release step, not a test, and it
 * repeats whenever the merchant account changes.
 */
export function qrisMerchantName(): string {
  return process.env.NEXT_PUBLIC_QRIS_MERCHANT_NAME ?? "";
}

export function qrisMerchantId(): string {
  return process.env.NEXT_PUBLIC_QRIS_MERCHANT_ID ?? "";
}

export function qrisTerminalLabel(): string {
  return process.env.NEXT_PUBLIC_QRIS_TERMINAL_LABEL ?? "";
}

/**
 * The two footer fields on the standard QRIS frame: the acquirer that issued
 * the code, and the version of the printed layout.
 *
 * Configured for the same reason as the identity above, and rendered only when
 * set. They belong to the acquirer rather than to us, so an unset value shows
 * nothing — an invented acquirer code on a payment surface would be worse than
 * a missing line.
 */
export function qrisAcquirerCode(): string {
  return process.env.NEXT_PUBLIC_QRIS_ACQUIRER_CODE ?? "";
}

export function qrisPrintVersion(): string {
  return process.env.NEXT_PUBLIC_QRIS_PRINT_VERSION ?? "";
}
