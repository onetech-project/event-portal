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
  /**
   * Admin-authored note shown on the booking card in place of the standard
   * non-refundable wording. Null falls back to it.
   */
  description: string | null;
  price: string;
  /** The remaining quota. 0 means sold out. */
  quota_remaining: number;
  sales_start: string;
  sales_end: string;
};

/**
 * Content-only detail (clarification 2026-08-05): ticket and package data live
 * behind GET /ticket/:event_id and GET /packages/:event_id, fetched only on
 * the ticket selection page. description carries sanitized HTML.
 */
export type EventDetail = EventSummary & {
  description: string | null;
  /** Expected visitor count, rendered as "{count}+ Visitors"; null hides it. */
  scale: number | null;
  /** CMS content blocks, position-ordered; always arrays, never null. */
  activities: ActivityBlock[];
  guest_stars: GuestStarBlock[];
  guidelines: GuidelineBlock[];
  /** False disables Buy Ticket — the event has no authored T&C yet. */
  has_terms: boolean;
};

export type PackageComponentSummary = {
  ticket_type_id: string;
  ticket_type_name: string;
  quantity_per_unit: number;
};

export type PackageSummary = {
  id: string;
  name: string;
  description: string | null;
  /** Money as decimal string, e.g. "50000.00". */
  price: string;
  sales_start: string;
  sales_end: string;
  /** Derived at read time from constituent quota — never cached. */
  available_units: number;
  /** All gates folded: status active, sales window open, availability > 0. */
  purchasable: boolean;
  components: PackageComponentSummary[];
};

/** One selected line. Exactly one id is set, mirroring the server's XOR. */
export type CheckoutItemInput = (
  | { ticket_type_id: string; package_id?: never }
  | { ticket_type_id?: never; package_id: string }
) & { quantity: number };

/**
 * One row of the booking list. Discriminated so the list can badge bundles and
 * read the right availability ceiling without inspecting shape.
 */
export type SelectableItem =
  | { kind: "ticket"; id: string; ticket: TicketTypeSummary }
  | { kind: "package"; id: string; pkg: PackageSummary };

/** One line of the running Selected Ticket summary. */
export type SelectionLine = {
  kind: "ticket" | "package";
  id: string;
  name: string;
  /** Money as decimal string. */
  unitPrice: string;
  quantity: number;
};

/**
 * One row of GET /ticket/genders — the gender master list the registration
 * forms build their options from. `name` is the canonical stored value.
 *
 * `id` narrowed from a uuid string to a number in migration 0013. Nothing reads
 * it as data — the form's select binds to `name`, which is also what checkout
 * submits — so it survives only as a React key.
 */
export type GenderOption = { id: number; name: string };

/** The current Terms & Conditions document shown by the booking dialog. */
export type EventTerms = {
  id: string;
  /** Sanitized HTML authored in the admin CMS. */
  content: string;
  updated_at: string | null;
};

/** POST /ticket/book 201 data — the held order (1-hour hold, no payment yet). */
export type BookResponse = {
  /** The public order number, used in every later /ticket/... path. */
  order_id: string;
  status: string;
  total_amount: string;
  expires_at: string;
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
  kind: "ticket" | "package";
  /** Non-null only when kind is "ticket". */
  ticket_type_name: string | null;
  /** Non-null only when kind is "package". */
  package_name: string | null;
  quantity: number;
  unit_price: string;
  subtotal: string;
};

/**
 * The body of POST /ticket/resend-email.
 *
 * Deliberately says nothing else: the endpoint is unauthenticated, so it names
 * neither the recipient nor whether the order exists, and answers the same way
 * either side of both.
 */
export type PublicResendResponse = { message: string };

/** One attendee slot on the 008 guest order read — details null until checkout. */
export type TicketOrderSlot = {
  id: string;
  ticket_type_name: string;
  package_name: string | null;
  /** Package origin id; null for standalone-ticket slots. */
  package_id: string | null;
  /**
   * Ordinal of the purchased bundle unit this slot belongs to (1-based) —
   * one visitor form fills a whole unit (spec 010). Null for standalone slots
   * and for bundle slots booked before spec 010 (those render one form each).
   */
  package_unit: number | null;
  name: string | null;
  email: string | null;
  phone: string | null;
  /** Date-only, YYYY-MM-DD. */
  dob: string | null;
  gender: string | null;
};

/**
 * The 008 guest order read (GET /ticket/order/:order_id) — everything the
 * order screens route on: `payment_started` picks forms vs QR panel,
 * `expires_at` drives the live countdown (hold before payment, 14-minute
 * window after).
 */
/** One frozen fee line on the guest order read, e.g. { name: "PPN (11%)" }. */
export type PublicOrderFee = { name: string; amount: string };

export type TicketOrderDetail = {
  order_id: string;
  status: OrderStatus;
  total_amount: string;
  /** Pre-fee sum of the lines; null on orders that predate fees. */
  subtotal: string | null;
  /** The frozen breakdown between subtotal and total (Figma 32-1366). */
  fees: PublicOrderFee[];
  expires_at: string | null;
  terms_agreed_at: string | null;
  payment_started: boolean;
  /** Venue, address, and dates feed the registration page's event box. */
  event: {
    name: string;
    slug: string;
    venue: string;
    address: string;
    start_date: string;
    end_date: string;
  };
  items: PublicOrderItem[];
  slots: TicketOrderSlot[];
  server_time: string;
  payment: PaymentInstruction | null;
};

/**
 * POST /ticket/checkout/:order_id 200 data.
 *
 * `qr_refresh_after_seconds` is gone: one order gets one code for one window,
 * and `expires_at` is now the gateway's own deadline rather than a figure this
 * system computed and hoped the gateway would honour.
 */
export type CheckoutQRResponse = {
  order_id: string;
  qr_string: string;
  expires_at: string;
  qr_image_url: string;
};

/** One SSE frame from GET /ticket/checkout/:order_id/status (unenveloped). */
export type CheckoutStatusEvent = {
  order_id: string;
  status: string;
  expires_at?: string;
};

/**
 * One row of GET /admin/payment/order/:order_id/notifications.
 *
 * Accepted and refused notifications both appear, newest first. A refused one is
 * often the whole explanation, so a history that showed only what was accepted
 * would hide the reason an order is stuck.
 */
export type PaymentNotification = {
  id: string;
  provider: string;
  transaction_id: string;
  /** The gateway's raw status, or one of this system's own markers. */
  status: string;
  /** Separates this system's own conclusions from what the gateway said. */
  is_marker: boolean;
  payment_type: string;
  raw_payload: unknown;
  received_at: string;
};

/**
 * One row of GET /admin/payment/order/:order_id/holds — what the order holds of
 * a ticket type against what that type has left.
 *
 * Both numbers matter: the top-up an operator needs before asking the gateway to
 * resend is the difference, and `remaining` alone looks reassuring right up
 * until it is smaller than `held`.
 */
export type PaymentOrderHold = {
  ticket_type_id: string;
  ticket_type_name: string;
  held: number;
  remaining: number;
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
  /** Expected visitor count for the detail info bar. */
  scale: number | null;
  created_at: string | null;
  updated_at: string | null;
};

export type TicketTypeAdminView = {
  id: string;
  event_id: string;
  name: string;
  /** Remark shown on the booking card in place of the standard notice. */
  description: string | null;
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

/**
 * The administrator's view of a package.
 *
 * It has no quota field, deliberately: a package owns no inventory, and
 * `available_units` is derived from its constituents on every read.
 */
export type PackageAdminView = {
  id: string;
  event_id: string;
  name: string;
  description: string | null;
  price: string;
  sales_start: string;
  sales_end: string;
  /**
   * Availability as a two-state flag (spec 011 FR-029), replacing the
   * ACTIVE|INACTIVE string. The order status deliberately went the other way and
   * keeps its name on the wire — its names come from a master list that has to
   * round-trip, which a package's flag does not.
   */
  is_active: boolean;
  components: PackageComponentSummary[];
  /** Derived, read-only. Whole sets the constituents can still cover. */
  available_units: number;
  /** Derived from order lines, never stored. */
  sold: number;
  created_at: string | null;
  updated_at: string | null;
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
  /** The buyer's address — the order's sole delivery recipient (spec 011 FR-012). */
  sent_to: string;
};

/** One fee master row on the admin panel (GET /admin/fees). */
export type FeeAdminView = {
  id: string;
  name: string;
  /** PERCENT applies value% of the subtotal; FIXED is a flat rupiah amount. */
  fee_type: "PERCENT" | "FIXED";
  /** Decimal string, e.g. "11.00" (percent) or "1200.00" (fixed). */
  value: string;
  position: number;
  is_active: boolean;
  created_at: string | null;
  updated_at: string | null;
};

// Icon keys for content blocks are lucide slugs validated server-side against
// the generated catalog (spec 009) — the picker and the backend share
// backend/internal/event/iconkeys/icon_keys.txt, so no key list lives here.

/** One activity content block (admin + guest reads share the shape). */
export type ActivityBlock = {
  id: string;
  title: string;
  description: string;
  icon: string | null;
  position: number;
};

/** One guest-star content block. */
export type GuestStarBlock = {
  id: string;
  name: string;
  position: number;
};

/** One guideline content block. */
export type GuidelineBlock = {
  id: string;
  description: string;
  icon: string | null;
  position: number;
};
