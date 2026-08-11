/**
 * Drives payment outcomes the way the provider would: by POSTing a correctly
 * signed notification to the webhook.
 *
 * This is what makes the suite genuinely end to end rather than stopping at the
 * QR screen. Nothing here reaches into the database to flip a status — the order
 * moves because the same handler a real settlement would hit ran and moved it,
 * which means the whole post-payment chain (ticket issuance, PDF, email, SSE
 * push, cache invalidation) runs too.
 */

import { createHash } from "node:crypto";

import { config } from "./env";

/**
 * Midtrans signs notifications as
 *   sha512(order_id + status_code + gross_amount + server_key)
 * lower-case hex. `internal/payment/midtrans.go` verifies exactly this, in
 * constant time, so the suite has to reproduce it byte for byte.
 */
export function signNotification(
  orderId: string,
  statusCode: string,
  grossAmount: string,
): string {
  return createHash("sha512")
    .update(orderId + statusCode + grossAmount + config.midtransServerKey)
    .digest("hex");
}

type NotificationOptions = {
  orderId: string;
  /** Provider status: settlement, capture, expire, cancel, deny, failure. */
  transactionStatus: string;
  grossAmount: string;
  /** "200" for a settled payment, "202" for deny/cancel, "407" for expiry. */
  statusCode?: string;
  fraudStatus?: string;
  /** Corrupts the signature, to prove the webhook rejects what it cannot authenticate. */
  tamperSignature?: boolean;
};

export function buildNotification(options: NotificationOptions) {
  const statusCode = options.statusCode ?? "200";
  const signature = signNotification(options.orderId, statusCode, options.grossAmount);

  return {
    transaction_time: new Date().toISOString().replace("T", " ").slice(0, 19),
    transaction_status: options.transactionStatus,
    transaction_id: `stub-tx-${options.orderId}`,
    status_message: "midtrans notification (e2e)",
    status_code: statusCode,
    signature_key: options.tamperSignature ? "0".repeat(128) : signature,
    payment_type: "qris",
    order_id: options.orderId,
    gross_amount: options.grossAmount,
    fraud_status: options.fraudStatus ?? "accept",
  };
}

/**
 * Delivers a notification and returns the HTTP status, so a test can assert on
 * both the accepted and the rejected case.
 */
export async function deliverNotification(options: NotificationOptions): Promise<number> {
  const body = buildNotification(options);

  const response = await fetch(`${config.apiURL}/payment/webhook/midtrans`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });

  return response.status;
}

/** The happy path: the guest paid. */
export async function settleOrder(orderId: string, grossAmount: string): Promise<number> {
  return deliverNotification({
    orderId,
    transactionStatus: "settlement",
    grossAmount,
    statusCode: "200",
  });
}

/** The guest walked away and the provider expired the code; quota goes back. */
export async function expireOrder(orderId: string, grossAmount: string): Promise<number> {
  return deliverNotification({
    orderId,
    transactionStatus: "expire",
    grossAmount,
    statusCode: "407",
  });
}
