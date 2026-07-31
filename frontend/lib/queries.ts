"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { adminFetch, apiFetch } from "./api-client";
import type {
  AttendeeSummary,
  CheckoutRequest,
  EventAdminDetail,
  EventAdminView,
  EventDetail,
  EventSummary,
  LoginResponse,
  MarkUsedResponse,
  OrderResponse,
  OrderSummary,
  PublicTicket,
  ResendResponse,
  TicketTypeAdminView,
  ValidationResult,
} from "./types";

/**
 * Query keys, kept in one place so a mutation can invalidate exactly what it
 * changed rather than blowing away the whole cache.
 */
export const queryKeys = {
  events: ["events"] as const,
  event: (slug: string) => ["events", slug] as const,
  ticket: (code: string) => ["tickets", code] as const,
  adminEvents: ["admin", "events"] as const,
  adminEvent: (id: string) => ["admin", "events", id] as const,
  adminTicketTypes: (eventId: string) => ["admin", "ticket-types", eventId] as const,
  adminOrders: (status?: string, eventId?: string) =>
    ["admin", "orders", status ?? null, eventId ?? null] as const,
  adminAttendees: (orderId?: string, eventId?: string) =>
    ["admin", "attendees", orderId ?? null, eventId ?? null] as const,
};

// --- Guest ----------------------------------------------------------------

export function usePublishedEvents() {
  return useQuery({
    queryKey: queryKeys.events,
    queryFn: () => apiFetch<EventSummary[]>("/events"),
  });
}

export function useEventBySlug(slug: string) {
  return useQuery({
    queryKey: queryKeys.event(slug),
    queryFn: () => apiFetch<EventDetail>(`/events/${encodeURIComponent(slug)}`),
    enabled: slug !== "",
  });
}

export function useCheckout() {
  return useMutation({
    mutationFn: (body: CheckoutRequest) =>
      apiFetch<OrderResponse>("/checkout", { method: "POST", body }),
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
