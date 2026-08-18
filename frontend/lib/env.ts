import { runtimeConfig } from "./runtime-config";

/**
 * Runtime configuration for the browser bundle.
 *
 * Read through a function rather than a module constant so the value is never
 * frozen at import time: it comes from the config the server injects per
 * request ([lib/runtime-config.ts](./runtime-config.ts)), which is what lets one
 * image be promoted from UAT to production unchanged.
 */
export function apiBaseUrl(): string {
  return runtimeConfig().apiBaseUrl.replace(/\/$/, "");
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
 *
 * Supplied at run time like the API URL, so that check belongs to the
 * deployment rather than to the build: a promoted image carries no merchant
 * identity of its own, and an environment that forgets to set these shows an
 * empty frame rather than the previous environment's merchant.
 */
export function qrisMerchantName(): string {
  return runtimeConfig().qrisMerchantName;
}

export function qrisMerchantId(): string {
  return runtimeConfig().qrisMerchantId;
}

export function qrisTerminalLabel(): string {
  return runtimeConfig().qrisTerminalLabel;
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
  return runtimeConfig().qrisAcquirerCode;
}

export function qrisPrintVersion(): string {
  return runtimeConfig().qrisPrintVersion;
}
