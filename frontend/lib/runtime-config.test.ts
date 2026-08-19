import { afterEach, describe, expect, it } from "vitest";

import { apiBaseUrl, apiOrigin } from "./env";
import {
  RUNTIME_CONFIG_KEY,
  runtimeConfig,
  runtimeConfigFromEnv,
  runtimeConfigScript,
} from "./runtime-config";

/**
 * The runtime-over-build-time mechanism, exercised on `API_BASE_URL`.
 *
 * It used to be exercised on the five QRIS frame fields, which spec 020 retired
 * along with the frame text they fed. They were only ever the vehicle: what
 * matters is the resolution order — bare name, then `NEXT_PUBLIC_` name, then
 * the default — because that is what lets one image be promoted from UAT to
 * production without a rebuild. Deleting these cases with the fields would have
 * been coverage loss dressed up as cleanup, so they were moved rather than
 * dropped.
 */

const RUNTIME_NAMES = ["API_BASE_URL"];

afterEach(() => {
  delete window[RUNTIME_CONFIG_KEY];
  for (const name of RUNTIME_NAMES) {
    delete process.env[name];
    delete process.env[`NEXT_PUBLIC_${name}`];
  }
});

describe("runtimeConfigFromEnv", () => {
  it("prefers the runtime variable over the build-time one", () => {
    // The build inlined UAT; this container was started pointing at production.
    process.env.NEXT_PUBLIC_API_BASE_URL = "https://uat.example/api/v1";
    process.env.API_BASE_URL = "https://prod.example/api/v1";

    expect(runtimeConfigFromEnv()).toEqual({
      apiBaseUrl: "https://prod.example/api/v1",
    });
  });

  it("falls back to the build-time variable, so existing images keep working", () => {
    // vitest.setup.ts sets NEXT_PUBLIC_API_BASE_URL before every test.
    expect(runtimeConfigFromEnv()).toEqual({
      apiBaseUrl: "http://api.test/api/v1",
    });
  });

  it("treats an empty runtime variable as unset rather than as a value", () => {
    process.env.API_BASE_URL = "";

    expect(runtimeConfigFromEnv()).toEqual({
      apiBaseUrl: "http://api.test/api/v1",
    });
  });

  it("falls back to the built-in default when nothing is configured at all", () => {
    delete process.env.NEXT_PUBLIC_API_BASE_URL;

    expect(runtimeConfigFromEnv()).toEqual({
      apiBaseUrl: "http://localhost:8080/api/v1",
    });
  });
});

describe("runtimeConfig", () => {
  it("uses the injected config, which is what makes one image promotable", () => {
    window[RUNTIME_CONFIG_KEY] = { apiBaseUrl: "https://uat.example/api/v1" };

    expect(apiBaseUrl()).toBe("https://uat.example/api/v1");
    expect(apiOrigin()).toBe("https://uat.example");
  });

  it("ignores an injected config that carries no base URL", () => {
    // apiBaseUrl doubles as the marker for "the server really did inject this".
    window[RUNTIME_CONFIG_KEY] = {};

    expect(runtimeConfig().apiBaseUrl).toBe("http://api.test/api/v1");
  });

  it("strips a trailing slash so paths do not double up", () => {
    window[RUNTIME_CONFIG_KEY] = { apiBaseUrl: "https://uat.example/api/v1/" };

    expect(apiBaseUrl()).toBe("https://uat.example/api/v1");
  });

  // Not covered, and deliberately noted rather than quietly missing: an
  // injected field that arrives EMPTY must still win over the environment, so
  // that a deployment which unsets something does not silently inherit the
  // build's value. With `apiBaseUrl` the only field, and that field doubling as
  // the "was this injected at all" marker, the case has no observable form —
  // an empty base URL is indistinguishable from no injection. The spread in
  // runtimeConfig() that implements it is kept for the next field added, and
  // this comment is here so whoever adds one restores the test with it.
});

describe("runtimeConfigScript", () => {
  it("assigns the whole config to the agreed global", () => {
    const script = runtimeConfigScript({
      apiBaseUrl: "https://prod.example/api/v1",
    });

    expect(script).toBe(
      `window.${RUNTIME_CONFIG_KEY}=` +
        '{"apiBaseUrl":"https://prod.example/api/v1"};',
    );
  });

  it("escapes '<' so a value can never close the script element early", () => {
    // The payload is interpolated into an inline <script>; a value carrying
    // </script> would otherwise turn configuration into markup.
    const script = runtimeConfigScript({
      apiBaseUrl: "</script><script>alert(1)</script>",
    });

    expect(script).not.toContain("</script>");
    expect(script).toContain("\\u003c/script>");
  });
});
