import { GlobalRegistrator } from "@happy-dom/global-registrator";

// Install window/document/localStorage before anything else imports React or the
// testing library, both of which capture globals at module load.
if (typeof globalThis.document === "undefined") {
  GlobalRegistrator.register({ url: "http://localhost:3000" });
}

const { cleanup } = await import("@testing-library/react");
await import("@testing-library/jest-dom/vitest");
const { afterEach, beforeEach, vi } = await import("vitest");

beforeEach(() => {
  // Every test starts from a known API base URL and an empty token store, so no
  // test can leak an authenticated session into the next one.
  process.env.NEXT_PUBLIC_API_BASE_URL = "http://api.test/api/v1";
  window.localStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
