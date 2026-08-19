/**
 * Configuration the container reads when it *starts*, not when the image was
 * built.
 *
 * Next inlines every `NEXT_PUBLIC_*` value into the JavaScript it ships, which
 * welds an image to one environment: a UAT build literally contains the UAT API
 * URL as a string constant, so the artifact UAT signed off on can never be the
 * artifact production runs. Promotion becomes a rebuild, and a rebuild is a
 * different binary than the one that was tested.
 *
 * So the server reads these values out of its own environment per request and
 * writes them into the document as a global; the browser reads them back from
 * there. One image, a different environment per deployment, no rebuild.
 *
 * Everything the browser needs to be configured with belongs here. A value left
 * on the `NEXT_PUBLIC_*` path is a value that quietly re-welds the image: it is
 * inlined as a string constant at build time, so the artifact UAT signed off on
 * can never be the artifact production runs.
 */

/** The global the server writes and the browser reads. */
export const RUNTIME_CONFIG_KEY = "__TICKETING_RUNTIME_CONFIG__";

export type RuntimeConfig = {
  /** Base URL of the Go API, including the `/api/v1` prefix. */
  apiBaseUrl: string;
};

/*
 * One field, for now. The five QRIS frame values that used to live here were
 * retired with the frame text they fed (spec 020). Nothing reads them any more,
 * so a deployment still exporting `QRIS_MERCHANT_NAME` and friends is simply
 * not consulted for them — no warning, no failure.
 *
 * The type is kept rather than collapsed to a bare string: the injection
 * mechanism, the `<` escaping below, and the resolution order above are all
 * still needed, and this is where the next per-environment value belongs.
 */

const FALLBACK_API_BASE_URL = "http://localhost:8080/api/v1";

declare global {
  interface Window {
    [RUNTIME_CONFIG_KEY]?: Partial<RuntimeConfig>;
  }
}

/**
 * Resolves the config from the process environment.
 *
 * The bare name is the runtime knob and wins. The `NEXT_PUBLIC_`-prefixed name
 * is kept as a fallback so `next dev`, the unit suite and any image already
 * built with a build arg behave exactly as they did before — but it is the
 * legacy path, and it cannot be changed without a rebuild.
 *
 * Only meaningful on the server. In the browser bundle the bare names are not
 * inlined by Next, which is precisely why the values have to travel through the
 * document instead.
 */
export function runtimeConfigFromEnv(): RuntimeConfig {
  const env = typeof process === "undefined" ? undefined : process.env;

  /** Runtime name first, legacy build-time name second, caller's default last. */
  const read = (name: string, fallback = ""): string =>
    env?.[name] || env?.[`NEXT_PUBLIC_${name}`] || fallback;

  return {
    apiBaseUrl: read("API_BASE_URL", FALLBACK_API_BASE_URL),
  };
}

/**
 * The effective config for the caller, whichever side of the wire it is on.
 *
 * Every field the injected config carries wins, including one that arrives
 * empty: the server has already resolved the fallbacks, so an empty value there
 * is genuinely unset and must not be quietly refilled from a build-time
 * constant. The environment covers the rest — the server itself, the unit suite,
 * the brief window before the injected script has run, and any field added to
 * the type after a payload was written.
 */
export function runtimeConfig(): RuntimeConfig {
  if (typeof window !== "undefined") {
    const injected = window[RUNTIME_CONFIG_KEY];

    // apiBaseUrl is the one field with no meaningful empty value, so it doubles
    // as the marker for "the server really did inject this".
    if (injected?.apiBaseUrl) {
      return { ...runtimeConfigFromEnv(), ...injected, apiBaseUrl: injected.apiBaseUrl };
    }
  }

  return runtimeConfigFromEnv();
}

/**
 * The script body that carries the config into the document.
 *
 * `<` is escaped because the payload is interpolated into an inline `<script>`:
 * a value containing `</script>` would otherwise close the element early and
 * turn configuration into markup.
 */
export function runtimeConfigScript(config: RuntimeConfig): string {
  const json = JSON.stringify(config).replace(/</g, "\\u003c");
  return `window.${RUNTIME_CONFIG_KEY}=${json};`;
}
