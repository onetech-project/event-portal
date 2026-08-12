/**
 * A thin client over the admin API, used to arrange the catalogue a scenario
 * needs and to read back state the UI does not surface.
 *
 * Arranging via the real API rather than SQL is deliberate: it means the setup
 * itself exercises the admin write paths, and — since those writes are what
 * invalidate the cache — a scenario that arranges through them is testing the
 * same invalidation the product depends on.
 */

import { config } from "./env";

export type ApiResult<T> = { status: number; data: T };

async function request<T>(
  path: string,
  init: RequestInit & { token?: string } = {},
): Promise<ApiResult<T>> {
  const { token, headers, ...rest } = init;

  const response = await fetch(`${config.apiURL}${path}`, {
    ...rest,
    headers: {
      "content-type": "application/json",
      ...(token ? { authorization: `Bearer ${token}` } : {}),
      ...headers,
    },
  });

  const text = await response.text();
  const body = text ? JSON.parse(text) : {};
  // The envelope is { data } on success and { code, message, data } on failure.
  return { status: response.status, data: (body.data ?? body) as T };
}

export async function adminLogin(): Promise<string> {
  const { status, data } = await request<{ token: string }>("/admin/login", {
    method: "POST",
    body: JSON.stringify({ email: config.admin.email, password: config.admin.password }),
  });
  if (status !== 200) {
    throw new Error(`admin login failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data.token;
}

export type SeededEvent = {
  id: string;
  slug: string;
  name: string;
};

export type SeededTicketType = {
  id: string;
  name: string;
  quota: number;
};

export function isoDaysFromNow(days: number): string {
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString();
}

export function isoHoursFromNow(hours: number): string {
  return new Date(Date.now() + hours * 60 * 60 * 1000).toISOString();
}

/**
 * The default admission window for a ticket type seeded against a
 * default-dated event (spec 015).
 *
 * Deliberately an hour INSIDE the event's +30d/+31d span rather than equal to
 * it. The event row and the ticket row are created by separate calls that read
 * the clock at different moments, so a nominally identical +31d would land a
 * few milliseconds AFTER the event's end and trip the containment check
 * (FR-005). Callers who need an exact match pass the event's own dates
 * explicitly, as createSellableEvent does.
 */
export function defaultEventWindow(): { start: string; end: string } {
  return { start: isoHoursFromNow(30 * 24 + 1), end: isoHoursFromNow(31 * 24 - 1) };
}

/**
 * Creates a published event with terms and one ticket type — the minimum a guest
 * needs to complete a purchase.
 *
 * Terms are not optional: booking refuses an event with no authored terms
 * (409001), because there would be nothing for the guest to have agreed to.
 */
export async function createSellableEvent(
  token: string,
  options: {
    slug: string;
    name?: string;
    quota?: number;
    price?: string;
    ticketName?: string;
    status?: "PUBLISHED" | "DRAFT";
    /**
     * The EVENT's own dates. Default to the standard +30d/+31d. Override both
     * together to seed an event that is running now — a validation scenario
     * needs one, because a ticket's admission window must sit inside its event
     * (spec 015 FR-005) and validation admits no tolerance (FR-014).
     */
    startDate?: string;
    endDate?: string;
    /** The ticket's admission window. Defaults to the event's own dates. */
    eventStart?: string;
    eventEnd?: string;
  },
): Promise<{ event: SeededEvent; ticketType: SeededTicketType }> {
  const startDate = options.startDate ?? isoDaysFromNow(30);
  const endDate = options.endDate ?? isoDaysFromNow(31);

  const event = await createEvent(token, {
    slug: options.slug,
    name: options.name ?? `UAT ${options.slug}`,
    status: options.status ?? "PUBLISHED",
    startDate,
    endDate,
  });

  await putTerms(token, event.id, "<p>These are the UAT terms and conditions.</p>");

  const ticketType = await createTicketType(token, {
    eventId: event.id,
    name: options.ticketName ?? "Regular",
    price: options.price ?? "150000.00",
    quota: options.quota ?? 10,
    eventStart: options.eventStart ?? startDate,
    eventEnd: options.eventEnd ?? endDate,
  });

  return { event, ticketType };
}

export async function createEvent(
  token: string,
  options: {
    slug: string;
    name: string;
    status?: "PUBLISHED" | "DRAFT";
    startDate?: string;
    endDate?: string;
  },
): Promise<SeededEvent> {
  const { status, data } = await request<SeededEvent>("/admin/events", {
    method: "POST",
    token,
    body: JSON.stringify({
      name: options.name,
      slug: options.slug,
      description: "Created by the UAT suite.",
      venue: "UAT Arena",
      address: "1 Test Street",
      start_date: options.startDate ?? isoDaysFromNow(30),
      end_date: options.endDate ?? isoDaysFromNow(31),
      banner_url: "https://placehold.co/1200x400/png",
      status: options.status ?? "PUBLISHED",
    }),
  });

  if (status !== 201) {
    throw new Error(`create event failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data;
}

export async function updateEvent(
  token: string,
  eventId: string,
  patch: Record<string, unknown>,
): Promise<ApiResult<SeededEvent>> {
  // The endpoint is a full replace, so the caller sends a complete body.
  return request<SeededEvent>(`/admin/events/${eventId}`, {
    method: "PUT",
    token,
    body: JSON.stringify({
      description: "Created by the UAT suite.",
      venue: "UAT Arena",
      address: "1 Test Street",
      start_date: isoDaysFromNow(30),
      end_date: isoDaysFromNow(31),
      banner_url: "https://placehold.co/1200x400/png",
      ...patch,
    }),
  });
}

export async function putTerms(token: string, eventId: string, content: string): Promise<void> {
  const { status, data } = await request(`/admin/events/${eventId}/terms`, {
    method: "PUT",
    token,
    body: JSON.stringify({ content }),
  });
  if (status !== 200) {
    throw new Error(`put terms failed with ${status}: ${JSON.stringify(data)}`);
  }
}

export type SeededPackage = {
  id: string;
  name: string;
};

/**
 * Creates a bundle over the given ticket types, one of each per unit.
 *
 * A package holds no inventory of its own — what it can sell is derived from its
 * constituents' remaining quota — so there is deliberately no quota argument
 * here, and sending one would be rejected by the API.
 */
export async function createPackage(
  token: string,
  options: {
    eventId: string;
    name: string;
    price: string;
    ticketTypeIds: string[];
    salesStart?: string;
    salesEnd?: string;
  },
): Promise<SeededPackage> {
  const { status, data } = await request<SeededPackage>("/admin/packages", {
    method: "POST",
    token,
    body: JSON.stringify({
      event_id: options.eventId,
      name: options.name,
      description: "Bundle created by the UAT suite.",
      price: options.price,
      sales_start: options.salesStart ?? isoDaysFromNow(-1),
      sales_end: options.salesEnd ?? isoDaysFromNow(29),
      is_active: true,
      components: options.ticketTypeIds.map((id) => ({
        ticket_type_id: id,
        quantity_per_unit: 1,
      })),
    }),
  });

  if (status !== 201) {
    throw new Error(`create package failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data;
}

export async function createTicketType(
  token: string,
  options: {
    eventId: string;
    name: string;
    price: string;
    quota: number;
    /** Defaults to a window that is open now; override to close it. */
    salesStart?: string;
    salesEnd?: string;
    /**
     * The admission window (spec 015). Required by the API, and must sit inside
     * the parent event's own dates. Defaults to the standard +30d/+31d event
     * span, which is what createEvent seeds.
     */
    eventStart?: string;
    eventEnd?: string;
  },
): Promise<SeededTicketType> {
  const { status, data } = await request<SeededTicketType>("/admin/ticket-types", {
    method: "POST",
    token,
    body: JSON.stringify({
      event_id: options.eventId,
      name: options.name,
      price: options.price,
      quota: options.quota,
      sales_start: options.salesStart ?? isoDaysFromNow(-1),
      sales_end: options.salesEnd ?? isoDaysFromNow(29),
      event_start: options.eventStart ?? defaultEventWindow().start,
      event_end: options.eventEnd ?? defaultEventWindow().end,
    }),
  });

  if (status !== 201) {
    throw new Error(`create ticket type failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data;
}

/**
 * Replaces a ticket type, so a scenario can shut its sales window under a guest
 * who is mid-selection. The endpoint is a full replace, so every field is sent.
 */
export async function updateTicketTypeWindow(
  token: string,
  ticketTypeId: string,
  options: {
    eventId: string;
    name: string;
    price: string;
    quota: number;
    salesStart: string;
    salesEnd: string;
    /** Full replace: omitting these would blank the admission window. */
    eventStart?: string;
    eventEnd?: string;
  },
): Promise<void> {
  const { status, data } = await request(`/admin/ticket-types/${ticketTypeId}`, {
    method: "PUT",
    token,
    body: JSON.stringify({
      event_id: options.eventId,
      name: options.name,
      price: options.price,
      quota: options.quota,
      sales_start: options.salesStart,
      sales_end: options.salesEnd,
      event_start: options.eventStart ?? defaultEventWindow().start,
      event_end: options.eventEnd ?? defaultEventWindow().end,
    }),
  });
  if (status !== 200) {
    throw new Error(`update ticket type failed with ${status}: ${JSON.stringify(data)}`);
  }
}

/**
 * Books against the public API as somebody else — the other buyer who takes the
 * seats while our guest is still choosing.
 *
 * Deliberately the real booking endpoint rather than an UPDATE on
 * `ticket_types.quota`: this is the call that holds the quota AND invalidates
 * the cached list, so a scenario arranged through it races the guest exactly
 * the way a second real guest would. Writing the quota directly would leave the
 * cache advertising seats that no longer exist and prove nothing.
 */
export async function bookAsAnotherGuest(
  eventId: string,
  ticketTypeId: string,
  quantity: number,
): Promise<{ order_id: string }> {
  const { status, data } = await request<{ order_id: string }>("/ticket/book", {
    method: "POST",
    body: JSON.stringify({
      event_id: eventId,
      items: [{ ticket_type_id: ticketTypeId, quantity }],
    }),
  });
  if (status !== 201) {
    throw new Error(`book failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data;
}

// --- Reads used for assertions ---------------------------------------------

export async function publicEventList(): Promise<Array<{ slug: string; name: string }>> {
  const { data } = await request<Array<{ slug: string; name: string }>>("/event");
  return data;
}

export async function publicTicketTypes(
  slug: string,
): Promise<Array<{
  id: string;
  name: string;
  quota_remaining: number;
  event_start: string;
  event_end: string;
}>> {
  const { data } = await request<Array<{
  id: string;
  name: string;
  quota_remaining: number;
  event_start: string;
  event_end: string;
}>>(
    `/ticket/${encodeURIComponent(slug)}`,
  );
  return data;
}

export async function adminOrders(
  token: string,
  filter: { status?: string; eventId?: string } = {},
): Promise<Array<{ order_number: string; status: string; buyer_name: string }>> {
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  if (filter.eventId) params.set("event_id", filter.eventId);
  const query = params.toString();

  const { data } = await request<Array<{ order_number: string; status: string; buyer_name: string }>>(
    `/admin/orders${query ? `?${query}` : ""}`,
    { token },
  );
  return data;
}

export type SeededFee = {
  id: string;
  name: string;
  fee_type: "PERCENT" | "FIXED";
  value: string;
};

/**
 * Add one fee to the master list, so an order books with `total_amount`
 * genuinely above its `subtotal`.
 *
 * Call this per-test rather than relying on the seed: migration 000010 inserts
 * `PPN (11%)` and `Admin Fee`, but `resetDatabase` TRUNCATEs `fees` along with
 * everything else (support/db.ts), so by the time a scenario runs there are no
 * fees at all and every order has `total_amount === subtotal`. A fee assertion
 * written against that state passes whatever the code does.
 */
export async function createFee(
  token: string,
  fee: {
    name: string;
    feeType: "PERCENT" | "FIXED";
    value: string;
    position?: number;
  },
): Promise<SeededFee> {
  const { status, data } = await request<SeededFee>("/admin/fees", {
    method: "POST",
    token,
    body: JSON.stringify({
      name: fee.name,
      fee_type: fee.feeType,
      value: fee.value,
      position: fee.position ?? 1,
      is_active: true,
    }),
  });
  if (status !== 201) {
    throw new Error(`create fee failed with ${status}: ${JSON.stringify(data)}`);
  }
  return data;
}

export type PublicOrder = {
  order_id: string;
  status: string;
  total_amount: string;
  subtotal: string | null;
  fees: Array<{ name: string; amount: string }>;
  payment: { amount: string } | null;
};

/**
 * The guest order read the order screens route on. Used here to assert against
 * the authoritative figures rather than against what the panel happens to
 * render — the point of spec 011 FR-016b is that the stored total does NOT move,
 * and only the API can say so.
 */
export async function publicOrder(orderNumber: string): Promise<PublicOrder> {
  const { status, data } = await request<PublicOrder>(
    `/ticket/order/${encodeURIComponent(orderNumber)}`,
  );
  if (status !== 200) {
    throw new Error(`read order ${orderNumber} failed with ${status}`);
  }
  return data;
}

export async function flushCache(token: string): Promise<ApiResult<{ status: string }>> {
  return request<{ status: string }>("/admin/cache/refresh", { method: "POST", token });
}

export async function health(): Promise<{ status: string; cache?: string }> {
  const response = await fetch(`${config.apiRootURL}/healthz`);
  return response.json();
}

/** Prometheus text exposition, for asserting cache hit/miss behaviour. */
export async function metrics(): Promise<string> {
  const response = await fetch(`${config.apiRootURL}/metrics`);
  return response.text();
}

export function metricValue(body: string, needle: string): number {
  const line = body.split("\n").find((l) => l.startsWith(needle));
  if (!line) return 0;
  return Number(line.slice(line.lastIndexOf(" ") + 1));
}
