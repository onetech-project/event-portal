import { describe, expect, it, vi } from "vitest";
import {
  clearToken,
  isAuthenticated,
  readSession,
  readToken,
  storeToken,
  subscribeToSession,
  takeSessionEndReason,
} from "./auth";

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

describe("session state", () => {
  it("reports authenticated for a live token", () => {
    storeToken("jwt-abc", inAnHour());

    expect(readSession()).toEqual({ status: "authenticated", token: "jwt-abc" });
  });

  it("reports anonymous when nothing is stored", () => {
    expect(readSession().status).toBe("anonymous");
  });

  it("reports anonymous with an expired reason once the token times out", () => {
    storeToken("jwt-old", anHourAgo());

    expect(readSession()).toEqual({ status: "anonymous", reason: "expired" });
  });

  it("reports anonymous for a tampered entry without throwing", () => {
    window.localStorage.setItem("ticketing.admin.token", "{{{");

    expect(() => readSession()).not.toThrow();
    expect(readSession().status).toBe("anonymous");
  });

  it("distinguishes a sign-out from an expiry", () => {
    storeToken("jwt-abc", inAnHour());
    clearToken();

    expect(readSession()).toEqual({ status: "anonymous", reason: "signed-out" });
  });

  // The login screen consumes the reason once; a later visit should not keep
  // claiming the session expired.
  it("consumes the end reason when read", () => {
    storeToken("jwt-old", anHourAgo());
    readSession();

    expect(takeSessionEndReason()).toBe("expired");
    expect(takeSessionEndReason()).toBeUndefined();
  });
});

describe("session change notification", () => {
  // The native storage event fires only in OTHER tabs. Without an in-page
  // signal, signing out would leave this tab rendering admin content until the
  // next navigation.
  it("notifies subscribers in the same tab on sign-out", () => {
    storeToken("jwt-abc", inAnHour());
    const listener = vi.fn();
    const unsubscribe = subscribeToSession(listener);

    clearToken();

    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });

  it("notifies subscribers in the same tab on sign-in", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeToSession(listener);

    storeToken("jwt-abc", inAnHour());

    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });

  it("stops notifying after unsubscribe", () => {
    const listener = vi.fn();
    subscribeToSession(listener)();

    storeToken("jwt-abc", inAnHour());

    expect(listener).not.toHaveBeenCalled();
  });

  it("reacts to another tab writing the same key", () => {
    const listener = vi.fn();
    const unsubscribe = subscribeToSession(listener);

    window.dispatchEvent(
      new StorageEvent("storage", { key: "ticketing.admin.token" }),
    );

    expect(listener).toHaveBeenCalled();
    unsubscribe();
  });
});
