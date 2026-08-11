"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { adminFetch, ApiError, apiFetch } from "./api-client";
import type {
  AttendeeSummary,
  AvailabilityDecision,
  BookResponse,
  CheckoutItemInput,
  EventTerms,
  EventAdminDetail,
  EventAdminView,
  EventDetail,
  EventSummary,
  FeeAdminView,
  GenderOption,
  LoginResponse,
  MarkUsedResponse,
  OrderSummary,
  PackageAdminView,
  PackageSummary,
  CheckoutQRResponse,
  PaymentRefreshResponse,
  PublicResendResponse,
  PublicTicket,
  ResendResponse,
  TicketOrderDetail,
  TicketTypeAdminView,
  TicketTypeSummary,
  ValidationResult,
} from "./types";

/**
 * Query keys, kept in one place so a mutation can invalidate exactly what it
 * changed rather than blowing away the whole cache.
 */
export const queryKeys = {
  events: ["events"] as const,
  event: (slug: string) => ["events", slug] as const,
  eventTicketTypes: (slug: string) => ["events", slug, "ticket-types"] as const,
  eventPackages: (slug: string) => ["events", slug, "packages"] as const,
  eventTerms: (slug: string) => ["events", slug, "terms"] as const,
  ticket: (code: string) => ["tickets", code] as const,
  order: (orderNumber: string) => ["orders", orderNumber] as const,
  genders: ["genders"] as const,
  adminEvents: ["admin", "events"] as const,
  adminEvent: (id: string) => ["admin", "events", id] as const,
  adminTicketTypes: (eventId: string) => ["admin", "ticket-types", eventId] as const,
  adminPackages: (eventId: string) => ["admin", "packages", eventId] as const,
  adminFees: ["admin", "fees"] as const,
  adminOrders: (status?: string, eventId?: string) =>
    ["admin", "orders", status ?? null, eventId ?? null] as const,
  adminAttendees: (orderId?: string, eventId?: string) =>
    ["admin", "attendees", orderId ?? null, eventId ?? null] as const,
};

// --- Guest ----------------------------------------------------------------

/**
 * The gender master list (GET /ticket/genders) the registration forms build
 * their options from — the set is data, not code (clarified 2026-08-05).
 */
export function useGenders() {
  return useQuery({
    queryKey: queryKeys.genders,
    queryFn: () => apiFetch<GenderOption[]>("/ticket/genders"),
    // Master data: changes only when an admin edits the table.
    staleTime: Infinity,
  });
}

export function usePublishedEvents() {
  return useQuery({
    queryKey: queryKeys.events,
    queryFn: () => apiFetch<EventSummary[]>("/event"),
  });
}

/** Content-only detail (clarification 2026-08-05) — no ticket/package data. */
export function useEventBySlug(slug: string) {
  return useQuery({
    queryKey: queryKeys.event(slug),
    queryFn: () => apiFetch<EventDetail>(`/event/${encodeURIComponent(slug)}`),
    enabled: slug !== "",
  });
}

/**
 * Live sellable ticket types for the selection page only. apiFetch already
 * sends no-store — quota is live inventory.
 */
export function useEventTicketTypes(slug: string) {
  return useQuery({
    queryKey: queryKeys.eventTicketTypes(slug),
    queryFn: () => apiFetch<TicketTypeSummary[]>(`/ticket/${encodeURIComponent(slug)}`),
    enabled: slug !== "",
  });
}

/** Active packages with derived availability, selection page only. */
export function useEventPackages(slug: string) {
  return useQuery({
    queryKey: queryKeys.eventPackages(slug),
    queryFn: () => apiFetch<PackageSummary[]>(`/packages/${encodeURIComponent(slug)}`),
    enabled: slug !== "",
  });
}

/**
 * The event's current Terms & Conditions, fetched when the dialog opens.
 *
 * No retry: an event without authored terms answers 404002 every time, and the
 * dialog words that case rather than spinning.
 */
export function useEventTerms(slug: string, enabled: boolean = true) {
  return useQuery({
    queryKey: queryKeys.eventTerms(slug),
    queryFn: () =>
      apiFetch<EventTerms>(`/ticket/terms-condition/${encodeURIComponent(slug)}`),
    enabled: enabled && slug !== "",
    retry: false,
  });
}

/**
 * Confirms the selection can still be bought, on the "Buy Ticket" press
 * (POST /ticket/availability, spec 013) — before the Terms & Conditions gate
 * opens, so a guest never reads a document for a purchase that cannot happen.
 *
 * The answer is advisory: it reserves nothing and creates no order, and a
 * refusal comes back as a 200 with `available: false` rather than as an error,
 * because it is a correct answer to a well-formed question. Only a malformed
 * request throws.
 */
export function useCheckAvailability() {
  return useMutation({
    mutationFn: (body: { event_id: string; items: CheckoutItemInput[] }) =>
      apiFetch<AvailabilityDecision>("/ticket/availability", { method: "POST", body }),
    // Same reasoning as useBookOrder: a retry on an ambiguous transport failure
    // doubles the load on an endpoint whose answer is stale the moment it lands.
    retry: false,
  });
}

/**
 * Books the selection on the T&C "Agree" click (POST /ticket/book): creates
 * the PENDING order and locks the seats for an hour. No payment involvement.
 */
export function useBookOrder() {
  return useMutation({
    mutationFn: (body: { event_id: string; items: CheckoutItemInput[] }) =>
      apiFetch<BookResponse>("/ticket/book", { method: "POST", body }),
    // A retry could double-book seats the first attempt already holds.
    retry: false,
  });
}

/**
 * Records the T&C agreement on a booked order — the second half of the same
 * Agree click. Idempotent server-side, so the dialog's retry path is safe.
 */
export function useRecordAgreement() {
  return useMutation({
    mutationFn: ({ orderId, eventTermsId }: { orderId: string; eventTermsId: string }) =>
      apiFetch<void>(`/ticket/terms-condition/${encodeURIComponent(orderId)}`, {
        method: "POST",
        body: { agreed: true, event_terms_id: eventTermsId },
      }),
  });
}

/** Order statuses that can never change again, so nothing needs to keep asking. */
const FINAL_ORDER_STATUSES: ReadonlySet<string> = new Set([
  "PAID",
  "CANCELLED",
  "EXPIRED",
]);

export function isFinalOrderStatus(status: string | undefined): boolean {
  return status !== undefined && FINAL_ORDER_STATUSES.has(status);
}

/** How often an unpaid order is re-read while its page is open. */
export const ORDER_POLL_INTERVAL_MS = 3_000;

/**
 * When the QR is re-issued, measured back from the payment deadline: with the
 * server's PAYMENT_WINDOW (14m) and QR_REFRESH_AFTER (7m) defaults, the refresh
 * lands 7 minutes before expiry. A checkout response carries the authoritative
 * `qr_refresh_after_seconds`; this constant covers page revisits, where only
 * `expires_at` is known.
 */
export const QR_REFRESH_BEFORE_EXPIRY_MS = 7 * 60_000;

/**
 * The guest's own order page.
 *
 * While the order is awaiting payment this polls every few seconds, which is
 * how the page flips itself to "paid" seconds after the provider's webhook
 * lands without the guest touching anything. Once the status is final the
 * interval is switched off: a settled page must place no further load on the
 * API.
 */
export function useOrderDetail(orderNumber: string) {
  return useQuery({
    queryKey: queryKeys.order(orderNumber),
    queryFn: () =>
      apiFetch<TicketOrderDetail>(`/ticket/order/${encodeURIComponent(orderNumber)}`),
    enabled: orderNumber !== "",
    // Live payment status: never serve it from a cache.
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchInterval: (query) =>
      isFinalOrderStatus(query.state.data?.status) ? false : ORDER_POLL_INTERVAL_MS,
    // Keep polling while the tab is backgrounded, so a guest who switched to
    // their banking app to pay comes back to an already-updated page.
    refetchIntervalInBackground: true,
    // An unknown order number stays unknown; retrying just repeats the 404.
    retry: (failureCount, error) =>
      !(error instanceof ApiError && error.status === 404) && failureCount < 3,
  });
}

/**
 * Asks the server to reconcile an order against the payment provider.
 *
 * This is the "check payment status" button. It exists because the webhook can
 * be delayed, lost, or — in local development — undeliverable, and a button
 * that only re-read our own database would be useless in exactly those cases.
 */
export function useRefreshPaymentStatus(orderNumber: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () =>
      apiFetch<PaymentRefreshResponse>(
        `/ticket/order/${encodeURIComponent(orderNumber)}/payment/refresh`,
        { method: "POST" },
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.order(orderNumber) }),
  });
}

/**
 * Re-sends a guest their own ticket email from the confirmation screen.
 *
 * Distinct from the admin `useResendTicketEmail` below: that one is keyed by the
 * order's UUID behind a token, this one by the order number a guest actually
 * holds, and it never learns where the mail went.
 *
 * No retry: the endpoint is rate limited to one send per order per minute, so an
 * automatic second attempt would only ever earn a 429 and make the button look
 * broken. The 429 is surfaced to the caller as a cooldown instead.
 */
/**
 * Starts payment for a held order (POST /ticket/checkout/:order_id): the
 * Option B call that carries the buyer + visitor forms and returns the QR.
 */
export function useStartCheckout(orderNumber: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      apiFetch<CheckoutQRResponse>(
        `/ticket/checkout/${encodeURIComponent(orderNumber)}`,
        { method: "POST", body },
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.order(orderNumber) }),
    // A blind retry could race the idempotent-return branch; the screen
    // decides what to do with each failure code instead.
    retry: false,
  });
}

/**
 * Re-issues the QR at the 7-minute mark (POST /ticket/checkout/:order_id/refresh-qr).
 * On failure the old QR stays on screen — the server kept it live too.
 */
export function useRefreshQR(orderNumber: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      apiFetch<CheckoutQRResponse>(
        `/ticket/checkout/${encodeURIComponent(orderNumber)}/refresh-qr`,
        { method: "POST" },
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.order(orderNumber) }),
    retry: false,
  });
}

export function useGuestResendTicketEmail(orderNumber: string) {
  return useMutation({
    mutationFn: () =>
      apiFetch<PublicResendResponse>(`/ticket/resend-email`, {
        method: "POST",
        body: JSON.stringify({ order_id: orderNumber }),
      }),
    retry: false,
  });
}

export function useTicketByCode(code: string) {
  return useQuery({
    queryKey: queryKeys.ticket(code),
    queryFn: () => apiFetch<PublicTicket>(`/tickets/${encodeURIComponent(code)}`),
    enabled: code !== "",
    // A wrong code will stay wrong; retrying only burns the endpoint's rate limit.
    retry: false,
  });
}

// --- Admin: auth ----------------------------------------------------------

export function useAdminLogin() {
  return useMutation({
    mutationFn: (body: { email: string; password: string }) =>
      apiFetch<LoginResponse>("/admin/login", { method: "POST", body }),
  });
}

// --- Admin: events --------------------------------------------------------

export function useAdminEvents() {
  return useQuery({
    queryKey: queryKeys.adminEvents,
    queryFn: () => adminFetch<EventAdminView[]>("/admin/events"),
  });
}

export function useAdminEvent(id: string) {
  return useQuery({
    queryKey: queryKeys.adminEvent(id),
    queryFn: () => adminFetch<EventAdminDetail>(`/admin/events/${id}`),
    enabled: id !== "",
  });
}

export function useCreateEvent() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<EventAdminView>("/admin/events", { method: "POST", body }),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.adminEvents }),
  });
}

export function useUpdateEvent(id: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<EventAdminView>(`/admin/events/${id}`, { method: "PUT", body }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: queryKeys.adminEvents });
      void client.invalidateQueries({ queryKey: queryKeys.adminEvent(id) });
    },
  });
}

export function useDeleteEvent() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => adminFetch<void>(`/admin/events/${id}`, { method: "DELETE" }),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.adminEvents }),
  });
}

// --- Admin: CMS content (spec 008 US4) -------------------------------------

/** The admin's view of an event's T&C document; 404002 while unauthored. */
export function useAdminEventTerms(eventId: string) {
  return useQuery({
    queryKey: [...queryKeys.adminEvent(eventId), "terms"] as const,
    queryFn: () => adminFetch<EventTerms>(`/admin/events/${eventId}/terms`),
    enabled: eventId !== "",
    // "Not authored yet" is a state, not a transient failure.
    retry: false,
  });
}

export function useUpsertEventTerms(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (content: string) =>
      adminFetch<EventTerms>(`/admin/events/${eventId}/terms`, {
        method: "PUT",
        body: { content },
      }),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: [...queryKeys.adminEvent(eventId), "terms"] }),
  });
}

/** The three content-block collections share one route shape. */
export type ContentBlockKind = "activities" | "guest-stars" | "guidelines";

function contentKey(eventId: string, kind: ContentBlockKind) {
  return [...queryKeys.adminEvent(eventId), "content", kind] as const;
}

export function useAdminContentBlocks<T>(eventId: string, kind: ContentBlockKind) {
  return useQuery({
    queryKey: contentKey(eventId, kind),
    queryFn: () => adminFetch<T[]>(`/admin/events/${eventId}/${kind}`),
    enabled: eventId !== "",
  });
}

export function useCreateContentBlock(eventId: string, kind: ContentBlockKind) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<unknown>(`/admin/events/${eventId}/${kind}`, { method: "POST", body }),
    onSuccess: () => client.invalidateQueries({ queryKey: contentKey(eventId, kind) }),
  });
}

export function useUpdateContentBlock(eventId: string, kind: ContentBlockKind) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: unknown }) =>
      adminFetch<void>(`/admin/events/${eventId}/${kind}/${id}`, { method: "PUT", body }),
    onSuccess: () => client.invalidateQueries({ queryKey: contentKey(eventId, kind) }),
  });
}

export function useDeleteContentBlock(eventId: string, kind: ContentBlockKind) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      adminFetch<void>(`/admin/events/${eventId}/${kind}/${id}`, { method: "DELETE" }),
    onSuccess: () => client.invalidateQueries({ queryKey: contentKey(eventId, kind) }),
  });
}

// --- Admin: ticket types (flat routes per the locked PRD §1.5) ------------

export function useAdminTicketTypes(eventId: string) {
  return useQuery({
    queryKey: queryKeys.adminTicketTypes(eventId),
    queryFn: () =>
      adminFetch<TicketTypeAdminView[]>(`/admin/ticket-types?event_id=${eventId}`),
    enabled: eventId !== "",
  });
}

export function useCreateTicketType(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<TicketTypeAdminView>("/admin/ticket-types", { method: "POST", body }),
    onSuccess: () => invalidateTicketTypes(client, eventId),
  });
}

export function useUpdateTicketType(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: unknown }) =>
      adminFetch<TicketTypeAdminView>(`/admin/ticket-types/${id}`, { method: "PUT", body }),
    onSuccess: () => invalidateTicketTypes(client, eventId),
  });
}

export function useDeleteTicketType(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      adminFetch<void>(`/admin/ticket-types/${id}`, { method: "DELETE" }),
    onSuccess: () => invalidateTicketTypes(client, eventId),
  });
}

// --- Admin: packages ------------------------------------------------------

export function useAdminPackages(eventId: string) {
  return useQuery({
    queryKey: queryKeys.adminPackages(eventId),
    // Availability is derived per request from live constituent quota, so this
    // must not be served from a stale cache.
    queryFn: () =>
      adminFetch<PackageAdminView[]>(
        `/admin/packages?event_id=${encodeURIComponent(eventId)}`,
      ),
    enabled: eventId !== "",
  });
}

export function useCreatePackage(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<PackageAdminView>("/admin/packages", { method: "POST", body }),
    onSuccess: () => invalidatePackages(client, eventId),
  });
}

export function useUpdatePackage(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: unknown }) =>
      adminFetch<PackageAdminView>(`/admin/packages/${id}`, { method: "PUT", body }),
    onSuccess: () => invalidatePackages(client, eventId),
  });
}

export function useDeletePackage(eventId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      adminFetch<void>(`/admin/packages/${id}`, { method: "DELETE" }),
    onSuccess: () => invalidatePackages(client, eventId),
  });
}

function invalidatePackages(client: ReturnType<typeof useQueryClient>, eventId: string) {
  void client.invalidateQueries({ queryKey: queryKeys.adminPackages(eventId) });
  void client.invalidateQueries({ queryKey: queryKeys.adminEvent(eventId) });
}

function invalidateTicketTypes(
  client: ReturnType<typeof useQueryClient>,
  eventId: string,
) {
  void client.invalidateQueries({ queryKey: queryKeys.adminTicketTypes(eventId) });
  void client.invalidateQueries({ queryKey: queryKeys.adminEvent(eventId) });
}

// --- Admin: read-only views ----------------------------------------------

export function useAdminOrders(status?: string, eventId?: string) {
  return useQuery({
    queryKey: queryKeys.adminOrders(status, eventId),
    queryFn: () => {
      const params = new URLSearchParams();
      if (status) params.set("status", status);
      if (eventId) params.set("event_id", eventId);
      const query = params.toString();
      return adminFetch<OrderSummary[]>(`/admin/orders${query ? `?${query}` : ""}`);
    },
  });
}

export function useAdminAttendees(orderId?: string, eventId?: string) {
  return useQuery({
    queryKey: queryKeys.adminAttendees(orderId, eventId),
    queryFn: () => {
      const params = new URLSearchParams();
      if (orderId) params.set("order_id", orderId);
      if (eventId) params.set("event_id", eventId);
      const query = params.toString();
      return adminFetch<AttendeeSummary[]>(`/admin/attendees${query ? `?${query}` : ""}`);
    },
  });
}

// --- Admin: door operations ----------------------------------------------

export function useValidateTicket() {
  return useMutation({
    mutationFn: (code: string) =>
      adminFetch<ValidationResult>("/admin/tickets/validate", {
        method: "POST",
        body: { code },
      }),
  });
}

export function useMarkTicketUsed() {
  return useMutation({
    mutationFn: (code: string) =>
      adminFetch<MarkUsedResponse>(
        `/admin/tickets/${encodeURIComponent(code.trim().toUpperCase())}/use`,
        { method: "POST" },
      ),
  });
}

export function useResendTicketEmail() {
  return useMutation({
    mutationFn: (orderId: string) =>
      adminFetch<ResendResponse>(`/admin/orders/${orderId}/resend-email`, {
        method: "POST",
      }),
  });
}

// --- Admin: fee master (clarified 2026-08-05) ------------------------------

export function useAdminFees() {
  return useQuery({
    queryKey: queryKeys.adminFees,
    queryFn: () => adminFetch<FeeAdminView[]>("/admin/fees"),
  });
}

export function useCreateFee() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<FeeAdminView>("/admin/fees", { method: "POST", body }),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.adminFees }),
  });
}

export function useUpdateFee(id: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) =>
      adminFetch<FeeAdminView>(`/admin/fees/${encodeURIComponent(id)}`, {
        method: "PUT",
        body,
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.adminFees }),
  });
}

export function useDeleteFee() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      adminFetch<{ message: string }>(`/admin/fees/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.adminFees }),
  });
}
