import { afterEach, describe, expect, it } from "vitest";

import {
  apiBaseUrl,
  apiOrigin,
  qrisAcquirerCode,
  qrisMerchantId,
  qrisMerchantName,
  qrisPrintVersion,
  qrisTerminalLabel,
} from "./env";
import {
  RUNTIME_CONFIG_KEY,
  runtimeConfig,
  runtimeConfigFromEnv,
  runtimeConfigScript,
} from "./runtime-config";

const RUNTIME_NAMES = [
  "API_BASE_URL",
  "QRIS_MERCHANT_NAME",
  "QRIS_MERCHANT_ID",
  "QRIS_TERMINAL_LABEL",
  "QRIS_ACQUIRER_CODE",
  "QRIS_PRINT_VERSION",
];

afterEach(() => {
  delete window[RUNTIME_CONFIG_KEY];
  for (const name of RUNTIME_NAMES) {
    delete process.env[name];
    delete process.env[`NEXT_PUBLIC_${name}`];
  }
});

describe("runtimeConfigFromEnv", () => {
  it("prefers the runtime variable over the build-time one", () => {
    process.env.API_BASE_URL = "https://prod.example/api/v1";
    process.env.NEXT_PUBLIC_QRIS_MERCHANT_NAME = "Stale Build Merchant";
    process.env.QRIS_MERCHANT_NAME = "Pupuk Kalteng";

    // vitest.setup.ts sets NEXT_PUBLIC_API_BASE_URL before every test.
    expect(runtimeConfigFromEnv()).toMatchObject({
      apiBaseUrl: "https://prod.example/api/v1",
      qrisMerchantName: "Pupuk Kalteng",
    });
  });

  it("falls back to the build-time variable, so existing images keep working", () => {
    process.env.NEXT_PUBLIC_QRIS_TERMINAL_LABEL = "659";

    expect(runtimeConfigFromEnv()).toMatchObject({
      apiBaseUrl: "http://api.test/api/v1",
      qrisTerminalLabel: "659",
    });
  });

  it("treats an empty runtime variable as unset rather than as a value", () => {
    process.env.API_BASE_URL = "";
    process.env.QRIS_MERCHANT_ID = "";
    process.env.NEXT_PUBLIC_QRIS_MERCHANT_ID = "936008580287697876";

    expect(runtimeConfigFromEnv()).toMatchObject({
      apiBaseUrl: "http://api.test/api/v1",
      qrisMerchantId: "936008580287697876",
    });
  });

  it("leaves an unconfigured QRIS field empty rather than inventing one", () => {
    // An invented acquirer code on a payment surface is worse than no line.
    expect(runtimeConfigFromEnv()).toMatchObject({
      qrisMerchantName: "",
      qrisMerchantId: "",
      qrisTerminalLabel: "",
      qrisAcquirerCode: "",
      qrisPrintVersion: "",
    });
  });
});

describe("runtimeConfig", () => {
  it("uses the injected config, which is what makes one image promotable", () => {
    window[RUNTIME_CONFIG_KEY] = {
      apiBaseUrl: "https://uat.example/api/v1",
      qrisMerchantName: "Pupuk Kalteng",
      qrisMerchantId: "936008580287697876",
      qrisTerminalLabel: "659",
      qrisAcquirerCode: "93600008",
      qrisPrintVersion: "1.0-2024.11.13",
    };

    expect(apiBaseUrl()).toBe("https://uat.example/api/v1");
    expect(apiOrigin()).toBe("https://uat.example");
    expect(qrisMerchantName()).toBe("Pupuk Kalteng");
    expect(qrisMerchantId()).toBe("936008580287697876");
    expect(qrisTerminalLabel()).toBe("659");
    expect(qrisAcquirerCode()).toBe("93600008");
    expect(qrisPrintVersion()).toBe("1.0-2024.11.13");
  });

  it("keeps an injected field empty instead of refilling it from the build", () => {
    // The build inlined a merchant name; this deployment deliberately has none.
    // Falling back here would print the previous environment's merchant on a
    // frame the guest is told to trust.
    process.env.NEXT_PUBLIC_QRIS_MERCHANT_NAME = "Stale Build Merchant";
    window[RUNTIME_CONFIG_KEY] = {
      apiBaseUrl: "https://uat.example/api/v1",
      qrisMerchantName: "",
    };

    expect(qrisMerchantName()).toBe("");
  });

  it("ignores an injected config that carries no base URL", () => {
    window[RUNTIME_CONFIG_KEY] = {};

    expect(runtimeConfig().apiBaseUrl).toBe("http://api.test/api/v1");
  });

  it("strips a trailing slash so paths do not double up", () => {
    window[RUNTIME_CONFIG_KEY] = { apiBaseUrl: "https://uat.example/api/v1/" };

    expect(apiBaseUrl()).toBe("https://uat.example/api/v1");
  });
});

describe("runtimeConfigScript", () => {
  it("assigns the whole config to the agreed global", () => {
    const script = runtimeConfigScript({
      apiBaseUrl: "https://prod.example/api/v1",
      qrisMerchantName: "Pupuk Kalteng",
      qrisMerchantId: "936008580287697876",
      qrisTerminalLabel: "659",
      qrisAcquirerCode: "93600008",
      qrisPrintVersion: "1.0-2024.11.13",
    });

    expect(script).toBe(
      `window.${RUNTIME_CONFIG_KEY}=` +
        '{"apiBaseUrl":"https://prod.example/api/v1",' +
        '"qrisMerchantName":"Pupuk Kalteng",' +
        '"qrisMerchantId":"936008580287697876",' +
        '"qrisTerminalLabel":"659",' +
        '"qrisAcquirerCode":"93600008",' +
        '"qrisPrintVersion":"1.0-2024.11.13"};',
    );
  });

  it("escapes '<' so a value can never close the script element early", () => {
    const script = runtimeConfigScript({
      ...runtimeConfigFromEnv(),
      qrisMerchantName: "</script><script>alert(1)</script>",
    });

    expect(script).not.toContain("</script>");
    expect(script).toContain("\\u003c/script>");
  });
});
