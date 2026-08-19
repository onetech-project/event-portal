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
