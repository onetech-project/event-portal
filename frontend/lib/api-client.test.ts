import { describe, expect, it, vi } from "vitest";
import { API_CODES, ApiError, apiFetch, adminFetch } from "./api-client";
import { clearToken, storeToken } from "./auth";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** A success body as the API sends it: {code, message, data} (spec 008). */
function envelope(data: unknown, status = 200) {
  return jsonResponse({ code: 200000, message: "Success", data }, status);
}

function mockFetch(response: Response) {
  const spy = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", spy);
  return spy;
}

describe("apiFetch", () => {
  it("resolves the path against the configured API base URL", async () => {
    const spy = mockFetch(envelope([{ slug: "jazz" }]));

    await apiFetch("/events");

    expect(spy).toHaveBeenCalledWith(
      "http://api.test/api/v1/events",
      expect.anything(),
    );
  });

  it("unwraps the envelope and returns its data", async () => {
    mockFetch(envelope([{ slug: "jazz-night" }]));

    const events = await apiFetch<{ slug: string }[]>("/events");

    expect(events).toEqual([{ slug: "jazz-night" }]);
  });

  // Event lists and quotas are live inventory: a cached response would show a
  // guest tickets that are already gone (constitution, Technology Stack).
  it("disables caching on GET requests", async () => {
    const spy = mockFetch(envelope([]));

    await apiFetch("/events");

    expect(spy.mock.calls[0][1]).toMatchObject({ cache: "no-store" });
  });

  it("sends a JSON body and content type on POST", async () => {
    const spy = mockFetch(envelope({ order_number: "ORD-1" }, 201));

    await apiFetch("/checkout", { method: "POST", body: { name: "Budi" } });

    const init = spy.mock.calls[0][1];
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ name: "Budi" }));
    expect(init.headers).toMatchObject({ "Content-Type": "application/json" });
  });

  // apiFetch owns serialization, and its `body?: unknown` signature makes a
  // caller that pre-stringifies look identical at the call site. It is not:
  // JSON.stringify of a string produces a quoted string, so the whole body
  // becomes a JSON string rather than an object and no server can bind it. That
  // is exactly how the guest ticket-email resend spent a release answering
  // "accepted" while sending nothing.
  it("sends an object body as a JSON object, not a JSON string", async () => {
    const spy = mockFetch(envelope({ message: "ok" }, 202));

    await apiFetch("/ticket/resend-email", {
      method: "POST",
      body: { order_id: "ORD-20260811-S8HD72" },
    });

    const sent: unknown = JSON.parse(spy.mock.calls[0][1].body);
    expect(typeof sent).toBe("object");
    expect(sent).toEqual({ order_id: "ORD-20260811-S8HD72" });
  });

  // The failure mode above, stated as its own assertion so a regression names
  // itself rather than surfacing as a puzzling 400 somewhere else.
  it("does not double-encode a body a caller already stringified", async () => {
    const spy = mockFetch(envelope({ message: "ok" }, 202));

    await apiFetch("/ticket/resend-email", {
      method: "POST",
      body: JSON.stringify({ order_id: "ORD-1" }),
    });

    // Documents the trap rather than blessing it: a pre-stringified body
    // arrives as a string, which is why callers must pass the object.
    expect(typeof JSON.parse(spy.mock.calls[0][1].body)).toBe("string");
  });

  it("throws an ApiError carrying the numeric envelope code", async () => {
    mockFetch(
      jsonResponse(
        { code: 400002, message: "Only 1 left.", data: null },
        400,
      ),
    );

    await expect(apiFetch("/checkout", { method: "POST" })).rejects.toMatchObject({
      status: 400,
      code: API_CODES.insufficientQuota,
      message: "Only 1 left.",
    });
  });

  it("still throws a usable ApiError when the body is not JSON", async () => {
    mockFetch(new Response("upstream exploded", { status: 502 }));

    const error = (await apiFetch("/checkout").catch((e: unknown) => e)) as ApiError;

    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(502);
    // Non-JSON bodies fall back to status*1000, keeping the numeric invariant.
    expect(error.code).toBe(502000);
    expect(error.message).not.toBe("");
  });

  it("returns undefined for a 204 with no body", async () => {
    mockFetch(new Response(null, { status: 204 }));

    await expect(apiFetch("/admin/events/1", { method: "DELETE" })).resolves.toBeUndefined();
  });

  it("reports a network failure as an ApiError rather than a raw TypeError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    const error = (await apiFetch("/events").catch((e: unknown) => e)) as ApiError;

    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(0);
    expect(error.code).toBe(API_CODES.network);
  });
});

describe("adminFetch", () => {
  it("attaches the stored bearer token", async () => {
    storeToken("jwt-abc", new Date(Date.now() + 3_600_000).toISOString());
    const spy = mockFetch(envelope([]));

    await adminFetch("/admin/events");

    expect(spy.mock.calls[0][1].headers).toMatchObject({
      Authorization: "Bearer jwt-abc",
    });
  });

  it("rejects before making a request when no token is stored", async () => {
    clearToken();
    const spy = mockFetch(envelope([]));

    await expect(adminFetch("/admin/events")).rejects.toBeInstanceOf(ApiError);
    expect(spy).not.toHaveBeenCalled();
  });

  // An expired session must be cleared, or every later request retries a token
  // the server will keep refusing.
  it("clears the stored token on a 401", async () => {
    storeToken("stale-jwt", new Date(Date.now() + 3_600_000).toISOString());
    mockFetch(jsonResponse({ code: 401001, message: "expired", data: null }, 401));

    await expect(adminFetch("/admin/events")).rejects.toMatchObject({ status: 401 });

    expect(window.localStorage.getItem("ticketing.admin.token")).toBeNull();
  });
});
