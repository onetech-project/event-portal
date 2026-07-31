import { describe, expect, it } from "vitest";
import { clearToken, isAuthenticated, readToken, storeToken } from "./auth";

const inAnHour = () => new Date(Date.now() + 3_600_000).toISOString();
const anHourAgo = () => new Date(Date.now() - 3_600_000).toISOString();

describe("admin token storage", () => {
  it("round-trips a stored token", () => {
    storeToken("jwt-abc", inAnHour());

    expect(readToken()).toBe("jwt-abc");
    expect(isAuthenticated()).toBe(true);
  });

  it("reports no token before login", () => {
    expect(readToken()).toBeNull();
    expect(isAuthenticated()).toBe(false);
  });

  it("clears the token on logout", () => {
    storeToken("jwt-abc", inAnHour());

    clearToken();

    expect(readToken()).toBeNull();
    expect(isAuthenticated()).toBe(false);
  });

  // Treating an expired token as absent sends the admin to the login screen
  // instead of through a request the server is certain to refuse.
  it("treats an expired token as absent", () => {
    storeToken("jwt-old", anHourAgo());

    expect(readToken()).toBeNull();
    expect(isAuthenticated()).toBe(false);
  });

  it("discards a corrupted entry rather than throwing", () => {
    window.localStorage.setItem("ticketing.admin.token", "not json");

    expect(readToken()).toBeNull();
  });

  it("discards an entry with no expiry", () => {
    window.localStorage.setItem(
      "ticketing.admin.token",
      JSON.stringify({ token: "jwt-abc" }),
    );

    expect(readToken()).toBeNull();
  });
});
