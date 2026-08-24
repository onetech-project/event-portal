/**
 * Drives payment outcomes the way the Manjo gateway would: by POSTing an
 * authenticated notification to the callback endpoint.
 *
 * This is what makes the suite genuinely end to end rather than stopping at the
 * QR screen. Nothing here reaches into the database to flip a status — the order
 * moves because the same handler a real settlement would hit ran and moved it,
 * which means the whole post-payment chain (ticket issuance, PDF, email, SSE
 * push, cache invalidation) runs too.
 */

import { config } from "./env";

/**
 * `status.Status` from the shared contract module, which is an integer enum.
 *
 * The numbers are the contract, not an implementation detail: the notification
 * carries `"s": 5`, and the API decodes it through the same enum. Spelling them
 * out here keeps the specs readable without pulling a Go module into a
 * TypeScript suite.
 *
 * Note that Pending is 0. A notification that omits `s` therefore decodes as
 * "pending" rather than failing — see `omitStatus` below.
 */
export const PaymentStatus = {
  Pending: 0,
  Reject: 1,
  Cancel: 2,
  Expired: 3,
  Obscure: 4,
  Completed: 5,
} as const;

export type PaymentStatusValue = (typeof PaymentStatus)[keyof typeof PaymentStatus];

/** `callback.TrxType`. DEPOSIT is money coming in; WITHDRAW is not ours. */
export const TrxType = {
  DEPOSIT: 0,
  WITHDRAW: 1,
} as const;

export type TrxTypeValue = (typeof TrxType)[keyof typeof TrxType];

type NotificationOptions = {
  orderNumber: string;
  /** The gateway's status enum. Settlement is Completed. */
  status?: PaymentStatusValue;
  /** The gateway's network transaction id (`nti`). */
  transactionId?: string;
  /** DEPOSIT unless a test is proving the withdrawal path changes nothing. */
  trxType?: TrxTypeValue;
  /**
   * Drops `s` from the body entirely, which is not the same as sending Pending:
   * the API distinguishes the two so an audit record cannot claim the gateway
   * said "pending" when it said nothing at all (FR-018).
   */
  omitStatus?: boolean;
  /**
   * Presents the wrong bearer token, to prove the callback rejects what it
   * cannot authenticate. There is no signature to tamper with — the token is
   * the whole mechanism.
   */
  invalidToken?: boolean;
};

/**
 * Builds a `callback.Request`. The JSON tags are heavily abbreviated on the
 * wire, which is why they are spelled out here rather than inline in a spec.
 */
export function buildNotification(options: NotificationOptions): Record<string, unknown> {
  const body: Record<string, unknown> = {
    ri: options.orderNumber,
    nti: options.transactionId ?? `stub-tx-${options.orderNumber}`,
    td: new Date().toISOString(),
    tft: "e2e-suite",
    tt: options.trxType ?? TrxType.DEPOSIT,
  };

  if (!options.omitStatus) {
    body.s = options.status ?? PaymentStatus.Completed;
  }

  return body;
}

/**
 * Delivers a notification and returns the HTTP status, so a test can assert on
 * both the accepted and the rejected case.
 *
 * The path is absolute and deliberately not under /api/v1: the gateway posts to
 * /v1.0/callback/exec, a path fixed by its own dispatch code (see CallbackPath
 * in `internal/payment/handler.go`).
 */
export async function deliverNotification(options: NotificationOptions): Promise<number> {
  const token = options.invalidToken
    ? `${config.pgCallbackToken}-wrong`
    : config.pgCallbackToken;

  const response = await fetch(`${config.apiRootURL}/v1.0/callback/exec`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(buildNotification(options)),
  });

  return response.status;
}

/** The happy path: the guest paid. */
export async function settleOrder(orderNumber: string): Promise<number> {
  return deliverNotification({ orderNumber, status: PaymentStatus.Completed });
}

/** The guest walked away and the gateway expired the code; quota goes back. */
export async function expireOrder(orderNumber: string): Promise<number> {
  return deliverNotification({ orderNumber, status: PaymentStatus.Expired });
}

/**
 * Arms the gateway stub to refuse the NEXT session open (spec 011 FR-030,
 * research R35).
 *
 * This is the only arrangement that reaches the state the restore exists for: a
 * checkout whose forms commit and whose gateway leg then fails. TX-D has already
 * written every holder's details by the time `CreateTransaction` is called, and
 * the failure compensates nothing — the order stays PENDING, keeps its hold, and
 * reports `payment_started: false` because no QR string was ever stamped. A
 * successful checkout cannot stand in for it: `payment_started` flips true and
 * the forms address forwards to /checkout.
 *
 * The stub answers 400 rather than 500 on purpose — the API retries a 5xx three
 * times under one reference, and a test wanting one clean failure should not
 * wait out the backoff.
 *
 * The refusal returns *before* the stub records the reference, so the retry that
 * follows is not a duplicate open: it succeeds, and the guest's seats are never
 * released.
 *
 * Lives here rather than in `gateway-stub.ts` because that module is the
 * standalone server — it imports `node:http` and nothing else, deliberately, so
 * Playwright's webServer can boot it without loading the suite's env.
 */
export async function failNextGatewaySession(): Promise<number> {
  const response = await fetch(`${config.gatewayStubURL}/__stub/fail-next-session`, {
    method: "POST",
  });
  return response.status;
}
