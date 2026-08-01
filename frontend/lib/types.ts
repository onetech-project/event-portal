/**
 * Wire types mirroring the API contracts under specs/.
 *
 * Money fields arrive as decimal strings (e.g. "150000.00") so no precision is
 * lost in JSON; format them with formatCurrency rather than doing arithmetic.
 */

export type EventSummary = {
  id: string;
  name: string;
  slug: string;
  venue: string;
  address: string;
  start_date: string;
  end_date: string;
  banner_url: string | null;
};

export type TicketTypeSummary = {
  id: string;
  name: string;
  price: string;
  /** The remaining quota. 0 means sold out. */
  quota_remaining: number;
  sales_start: string;
  sales_end: string;
};

export type EventDetail = EventSummary & {
  description: string | null;
  ticket_types: TicketTypeSummary[];
};

export type CheckoutRequest = {
  buyer_name: string;
  buyer_email: string;
  buyer_phone: string;
  items: { ticket_type_id: string; quantity: number }[];
  attendees: { ticket_type_id: string; name: string; email: string }[];
};

export type OrderResponse = {
  order_number: string;
  status: string;
  total_amount: string;
  /**
   * Retained for audit only. The guest is routed in-app to `/orders/{number}`;
   * nothing navigates here.
   */
  payment_url: string;
};

export type OrderStatus = "PENDING" | "PAID" | "CANCELLED" | "EXPIRED";

export type PaymentInstruction = {
  method: string;
  provider: string;
  amount: string;
  /** Server-owned deadline; the countdown is rendered against it. */
  expires_at: string;
  /** Path the QR image is rendered from, on demand. */
  qr_image_path: string;
};

export type PublicOrderItem = {
  ticket_type_name: string;
  quantity: number;
  unit_price: string;
  subtotal: string;
};

export type PublicOrderDetail = {
  order_number: string;
  status: OrderStatus;
  total_amount: string;
  buyer_name: string;
  buyer_email: string;
  created_at: string | null;
  event: { name: string; slug: string };
  items: PublicOrderItem[];
  /** The server's clock at response time, used to correct a wrong device clock. */
  server_time: string;
  /** Present only while the order is genuinely payable. */
  payment: PaymentInstruction | null;
};

export type PaymentRefreshResponse = {
  order_number: string;
  status: OrderStatus;
  changed: boolean;
  checked_at: string;
};

export type PublicTicket = {
  ticket_code: string;
  status: "ACTIVE" | "USED" | "REVOKED";
  event_name: string;
  attendee_name: string;
};

export type LoginResponse = {
  token: string;
  expires_at: string;
};

export type EventAdminView = {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  venue: string;
  address: string;
  start_date: string;
  end_date: string;
  banner_url: string | null;
  status: "DRAFT" | "PUBLISHED" | "COMPLETED";
  created_at: string | null;
  updated_at: string | null;
};

export type TicketTypeAdminView = {
  id: string;
  event_id: string;
  name: string;
  price: string;
  /** The remaining quota — the same live counter checkout decrements. */
  quota: number;
  /** Derived, read-only sold count. */
  sold: number;
  sales_start: string;
  sales_end: string;
};

export type EventAdminDetail = EventAdminView & {
  ticket_types: TicketTypeAdminView[];
};

export type OrderSummary = {
  id: string;
  order_number: string;
  buyer_name: string;
  buyer_email: string;
  status: string;
  total_amount: string;
  created_at: string | null;
};

export type AttendeeSummary = {
  name: string;
  email: string;
  ticket_type_name: string;
  order_number: string;
};

export type ValidationResult = {
  result: "VALID" | "ALREADY_USED" | "INVALID";
  ticket_code: string;
  attendee_name: string | null;
  ticket_type_name: string | null;
  event_name: string | null;
};

export type MarkUsedResponse = {
  ticket_code: string;
  status: string;
};

export type ResendResponse = {
  message: string;
  sent_to: string;
};
