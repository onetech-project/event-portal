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

export async function request<T>(
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

/**
 * Both deletes are guarded server-side and both return their refusal rather
 * than throwing, because the refusal is the interesting answer: an event whose
 * ticket types have been bought against is undeletable by design
 * (CodeEventHasOrders, admin_service.go), so a teardown running against a
 * deployment has to read the status and fall back to unpublishing.
 */
export async function deleteEvent(token: string, eventId: string): Promise<ApiResult<unknown>> {
  return request(`/admin/events/${eventId}`, { method: "DELETE", token });
}

export async function deleteTicketType(
  token: string,
  ticketTypeId: string,
): Promise<ApiResult<unknown>> {
  return request(`/admin/ticket-types/${ticketTypeId}`, { method: "DELETE", token });
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
    /**
     * Spec 022. FALSE makes this a REGISTRATION-ONLY type: off every guest
     * purchase surface, unbundleable, and obtainable only at
     * /events/:slug/register/:id — free, whatever price is set.
     *
     * Defaults to `true`, matching the column. Left undefined the key is not
     * sent at all, which the server also reads as visible — but the default is
     * stated here rather than relied upon, because under this column's polarity
     * the unsafe direction is the falsy one.
     */
    isVisible?: boolean;
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
      is_visible: options.isVisible ?? true,
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

/**
 * One page of an admin list, as every paginated admin endpoint returns it inside
 * the standard envelope's `data` (spec 021).
 */
export type PagedResponse<T> = {
  items: T[];
  page: number;
  page_size: number;
  total: number;
  total_pages: number;
};

export type Paging = { page?: number; pageSize?: number };

/** Adds paging to a query string, omitting what was not asked for. */
function withPaging(params: URLSearchParams, paging: Paging = {}): URLSearchParams {
  if (paging.page !== undefined) params.set("page", String(paging.page));
  if (paging.pageSize !== undefined) params.set("page_size", String(paging.pageSize));
  return params;
}

/**
 * Fetches one page of any admin list, returning the whole envelope payload so a
 * spec can assert on the total and the page count, not just the rows.
 */
export async function adminListPage<T>(
  token: string,
  path: string,
  params: URLSearchParams,
): Promise<PagedResponse<T>> {
  const query = params.toString();
  const { data } = await request<PagedResponse<T>>(`${path}${query ? `?${query}` : ""}`, {
    token,
  });
  return data;
}

export type AdminOrderRow = { order_number: string; status: string; buyer_name: string };

/**
 * One page of the admin order list. Returns the rows, which is what nearly every
 * caller wants; reach for `adminOrdersPage` when the total or the page count is
 * the thing under test.
 */
export async function adminOrders(
  token: string,
  filter: { status?: string; eventId?: string } & Paging = {},
): Promise<AdminOrderRow[]> {
  return (await adminOrdersPage(token, filter)).items;
}

/** One page of the admin order list, with its paging metadata intact. */
export async function adminOrdersPage(
  token: string,
  filter: { status?: string; eventId?: string } & Paging = {},
): Promise<PagedResponse<AdminOrderRow>> {
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  if (filter.eventId) params.set("event_id", filter.eventId);
  withPaging(params, filter);

  return adminListPage<AdminOrderRow>(token, "/admin/orders", params);
}

export type AdminAttendeeRow = {
  name: string;
  email: string;
  ticket_type_name: string;
  order_number: string;
};

/** One page of the admin attendee list, with its paging metadata intact. */
export async function adminAttendeesPage(
  token: string,
  filter: { orderId?: string; eventId?: string } & Paging = {},
): Promise<PagedResponse<AdminAttendeeRow>> {
  const params = new URLSearchParams();
  if (filter.orderId) params.set("order_id", filter.orderId);
  if (filter.eventId) params.set("event_id", filter.eventId);
  withPaging(params, filter);

  return adminListPage<AdminAttendeeRow>(token, "/admin/attendees", params);
}

/** One page of the admin event list, with its paging metadata intact. */
export async function adminEventsPage(
  token: string,
  paging: Paging = {},
): Promise<PagedResponse<{ id: string; name: string; slug: string }>> {
  return adminListPage(token, "/admin/events", withPaging(new URLSearchParams(), paging));
}

/**
 * Every event as an id/name pair — the selector read the filter dropdowns use.
 * Deliberately unpaginated, so a filter can always name every event.
 */
export async function adminEventOptions(
  token: string,
): Promise<Array<{ id: string; name: string }>> {
  const { data } = await request<Array<{ id: string; name: string }>>(
    "/admin/events/options",
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

export type PublicOrderSlot = {
  id: string;
  name: string | null;
  email: string | null;
  phone: string | null;
  /** Date-only, YYYY-MM-DD. */
  dob: string | null;
  /** What a restored form submits; the name beside it is what it displays. */
  gender_id: number | null;
  gender: string | null;
};

export type PublicOrder = {
  order_id: string;
  status: string;
  total_amount: string;
  subtotal: string | null;
  fees: Array<{ name: string; amount: string }>;
  payment: { amount: string } | null;
  /** False until a payment code has actually been stamped (spec 011 FR-030). */
  payment_started: boolean;
  /** Holder details as stored; null in every field until checkout saves them. */
  slots: PublicOrderSlot[];
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

/**
 * The gender master list (spec 023). Served from the read cache once warm, and
 * identical either way — which is the whole point of the scenarios that use it.
 */
/**
 * The identifier of a gender by name. Forms submit the identifier (spec 011
 * FR-034), and the seeded ids are not a contract — migration 0013 fixes the
 * ORDER STATUS ids only — so a spec asserting a literal would be encoding an
 * accident.
 */
export async function genderIdFor(name: string): Promise<number> {
  const match = (await publicGenders()).find((g) => g.name === name);
  if (match === undefined) throw new Error(`no gender named ${name} in the master list`);
  return match.id;
}

export async function publicGenders(): Promise<Array<{ id: number; name: string }>> {
  const { status, data } = await request<Array<{ id: number; name: string }>>("/ticket/genders");
  if (status !== 200) {
    throw new Error(`read genders failed with ${status}`);
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

/**
 * The guest-facing resend (spec 008 FR-024): the buyer asks for their own
 * tickets again, identified only by the order number.
 *
 * It answers 202 with the same bytes whatever it finds, so the status here says
 * nothing about whether mail went out — that is the point of the endpoint, and
 * why the delivery assertion reads Mailpit rather than this response.
 */
export async function resendTicketEmail(orderNumber: string): Promise<ApiResult<unknown>> {
  return request<unknown>("/ticket/resend-email", {
    method: "POST",
    body: JSON.stringify({ order_id: orderNumber }),
  });
}

/**
 * A Terms & Conditions document long enough that it must actually be SCROLLED.
 *
 * The default fixture is `<p>These are the UAT terms and conditions.</p>`, which
 * fits inside the dialog's reading area. The read-through gate treats a document
 * shorter than its viewport as already read (FR-014d) — correctly, since there is
 * nothing to scroll — so every scenario using the short fixture satisfies the gate
 * by being shown, and proves nothing about the gate.
 *
 * That is the "green because nobody looked" case Principle VIII exists to prevent:
 * the suite would pass identically against a build with the gate deleted. Any
 * scenario asserting the gate MUST seed this instead.
 *
 * 60 numbered clauses is comfortably past the dialog's max height at every
 * viewport the suite runs, without being so large it slows the render.
 */
export function longTermsHtml(clauses = 60): string {
  const items = Array.from(
    { length: clauses },
    (_, i) =>
      `<li>Clause ${i + 1}. This clause exists to make the document taller than the ` +
      `reading area, so the read-through gate has something to gate on.</li>`,
  ).join("");
  return `<ol>${items}<li id="last-clause">Final clause. You have reached the end.</li></ol>`;
}

/** Seeds an event whose terms are long enough to exercise the read-through gate. */
export async function putLongTerms(token: string, eventId: string): Promise<void> {
  await putTerms(token, eventId, longTermsHtml());
}

/**
 * A published event carrying BOTH a purchasable ticket type and a
 * registration-only one (spec 022), plus long terms.
 *
 * Both types on one event on purpose: it is the arrangement that catches
 * containment failing in either direction — the invitation type leaking onto the
 * guest list, or the filter being so eager it hides the purchasable one too.
 */
export async function createRegistrationEvent(
  token: string,
  options: {
    slug: string;
    name?: string;
    quota?: number;
    registrationQuota?: number;
    /**
     * Seed an event that is running NOW, so an issued ticket is inside its
     * admission window and validates as `Valid` rather than `NOT_YET_VALID`.
     *
     * Off by default: the standard +30d/+31d window is what every other scenario
     * wants, and a door-validation scenario is the exception. A ticket's
     * admission window must sit inside its event's own dates (spec 015 FR-005),
     * so both move together — which is why this is one flag and not two dates.
     */
    admittingNow?: boolean;
  },
): Promise<{
  event: SeededEvent;
  purchasable: SeededTicketType;
  registration: SeededTicketType;
}> {
  // Computed ONCE and reused, with the ticket window strictly INSIDE the event's.
  // Calling isoHoursFromNow twice would read the clock twice, and an admission
  // window equal to its parent's can land microseconds outside it — containment
  // then fails with a 400 that reads like a bug in the fixture rather than a
  // race (spec 015 FR-005).
  const eventStart = isoHoursFromNow(-2);
  const eventEnd = isoHoursFromNow(8);
  const admitStart = isoHoursFromNow(-1);
  const admitEnd = isoHoursFromNow(6);

  const { event, ticketType } = await createSellableEvent(token, {
    slug: options.slug,
    name: options.name,
    quota: options.quota ?? 10,
    ...(options.admittingNow
      ? { startDate: eventStart, endDate: eventEnd, eventStart: admitStart, eventEnd: admitEnd }
      : {}),
  });
  await putLongTerms(token, event.id);

  const registration = await createTicketType(token, {
    eventId: event.id,
    name: "Invitation Access",
    // Priced at zero by convention only — FR-003 makes the column independent of
    // price, and a registration charges nothing whatever is stored.
    price: "0.00",
    quota: options.registrationQuota ?? 5,
    isVisible: false,
    ...(options.admittingNow ? { eventStart: admitStart, eventEnd: admitEnd } : {}),
  });

  return { event, purchasable: ticketType, registration };
}

/**
 * The API's response with the ENVELOPE INTACT — `{ code, message, data }` — not
 * unwrapped the way `request` returns it.
 *
 * `request` returns `body.data ?? body`, which is right for reading a success
 * payload and wrong for comparing two refusals: every 404 carries `data: null`,
 * so two refusals compared through `request` are equal whatever their code and
 * message say. A scenario asserting that refusals are INDISTINGUISHABLE
 * (spec 022 FR-012) would then pass against a server that distinguished them
 * perfectly — the exact false green the assertion exists to prevent.
 */
export async function requestRaw(
  path: string,
  init: RequestInit & { token?: string } = {},
): Promise<{ status: number; body: unknown }> {
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
  return { status: response.status, body: text ? JSON.parse(text) : {} };
}
