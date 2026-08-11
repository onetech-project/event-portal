/**
 * A stand-in for the Manjo payment gateway, so the purchase flow can be
 * exercised end to end without the network, without sandbox credentials, and
 * without a human scanning a QR code.
 *
 * It implements exactly the one endpoint `internal/payment/manjo.go` calls:
 *
 *   POST /v1/manjo/transaction/incoming — opens a QRIS payment session
 *
 * and nothing else. There is deliberately no status-read endpoint: the Gateway
 * interface has exactly two methods, CreateTransaction and VerifyWebhook, so
 * the API never polls. Settlement arrives only as a callback, which is
 * `support/payment.ts`'s job.
 *
 * The shapes below are `paynet.TransactionResponse` and `method.QRResponse`
 * from the shared contract module. Three things are load-bearing:
 *
 *   - `s` (success) must be true. Only that makes it a session; anything else
 *     is a failed open whatever the HTTP status said (FR-003).
 *   - `d.mr.qr_r` must be non-empty. Checkout deliberately fails without a
 *     payload, because a payment page with no code is worse than a failure —
 *     failing releases the guest's seats so they can retry (FR-006).
 *   - `d.mr.qr_ea` must parse as RFC 3339. The contract types it as a
 *     timestamp, so an unreadable value costs the deadline (the API falls back
 *     to its configured window) though not the code itself.
 *
 * Run standalone: `node --experimental-strip-types support/gateway-stub.ts`
 */

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

/** `method.Method`. QR is the only one this shop opens sessions for. */
const METHOD_QR = 0;

/** What the stub remembers about each reference, so a duplicate open is refusable. */
type StubTransaction = {
  externalRefId: string;
  refId: string;
  amount: number;
};

const transactions = new Map<string, StubTransaction>();

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let raw = "";
    req.on("data", (chunk) => {
      raw += chunk;
    });
    req.on("end", () => resolve(raw));
    req.on("error", reject);
  });
}

function json(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, {
    "content-type": "application/json",
    "content-length": Buffer.byteLength(payload),
  });
  res.end(payload);
}

/**
 * Lets a test force the next session open to fail, so the compensating path —
 * the guest keeps their hold and their saved forms — can be exercised too.
 *
 * It answers 400 rather than 500 on purpose: the API retries a 5xx three times
 * under the same reference before giving up, which would make a test that wants
 * one clean failure wait out the backoff for no reason.
 */
let failNextSession = false;

export function createGatewayStub() {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://stub");

    // Test-only control surface. Namespaced under /__stub so it can never
    // collide with a real provider path.
    if (url.pathname === "/__stub/fail-next-session" && req.method === "POST") {
      failNextSession = true;
      return json(res, 200, { ok: true });
    }
    if (url.pathname === "/__stub/health") {
      return json(res, 200, { ok: true, transactions: transactions.size });
    }
    if (url.pathname === "/__stub/reset" && req.method === "POST") {
      transactions.clear();
      failNextSession = false;
      return json(res, 200, { ok: true });
    }

    if (url.pathname === "/v1/manjo/transaction/incoming" && req.method === "POST") {
      if (failNextSession) {
        failNextSession = false;
        return json(res, 400, { s: false, e: "stub: forced failure", d: null });
      }

      const body = JSON.parse((await readBody(req)) || "{}");
      const refId: string = body?.ri ?? "unknown";
      const amount: number = Number(body?.a ?? 0);

      // The gateway refuses a reference it has already issued a code for, and
      // the API leans on exactly that to make its session-open retry safe: a
      // retry can never produce a second live code for one order. Reproducing
      // the refusal keeps that guarantee honest rather than assumed.
      //
      // The wording matters. `isDuplicateReference` matches on text because the
      // contract types the error as `any`, and "duplicate" is one of the
      // markers it looks for.
      if (transactions.has(refId)) {
        return json(res, 200, {
          s: false,
          e: `duplicate ref id: ${refId}`,
          d: null,
        });
      }

      const externalRefId = `stub-eri-${refId}`;
      transactions.set(refId, { externalRefId, refId, amount });

      // Roughly the API's PAYMENT_EXPIRY, and deliberately not far from it.
      //
      // Under Manjo the gateway's expiry is adopted unchanged, whatever its
      // length (FR-009c) — it is not capped by the configured window the way a
      // Midtrans-era stub could assume. So this value *is* the deadline the
      // guest gets, and a wildly different one would both misrepresent the
      // gateway and trip the "differs materially" warning on every single
      // checkout, which is how a signal worth reading gets trained into noise.
      const expiresAt = new Date(Date.now() + 15 * 60_000).toISOString();

      return json(res, 200, {
        s: true,
        e: null,
        d: {
          m: METHOD_QR,
          eri: externalRefId,
          mr: {
            // A plausible QRIS payload. Only its non-emptiness is load-bearing:
            // the API renders the PNG itself from this string.
            qr_r: `00020101021226610014COM.STUB.WWW0118${refId}5204599953033605802ID6304ABCD`,
            qr_u: `http://localhost:8101/qr/${encodeURIComponent(refId)}`,
            qr_ea: expiresAt,
          },
        },
      });
    }

    // The provider-hosted QR image. Never fetched by the API — it generates its
    // own PNG — but a 404 here would be a confusing red herring in a trace.
    if (url.pathname.startsWith("/qr/")) {
      res.writeHead(200, { "content-type": "image/png" });
      return res.end(Buffer.from([0x89, 0x50, 0x4e, 0x47]));
    }

    json(res, 404, { s: false, e: "stub: no such path", d: null });
  });

  return {
    server,
    /** The amount the API opened the session for, for a test that wants to assert on it. */
    amountFor(refId: string): number | undefined {
      return transactions.get(refId)?.amount;
    },
    /** The gateway-side reference the API stores as the provider ref. */
    externalRefFor(refId: string): string | undefined {
      return transactions.get(refId)?.externalRefId;
    },
  };
}

// Standalone entrypoint, used by Playwright's webServer.
const isMain = process.argv[1]?.endsWith("gateway-stub.ts");
if (isMain) {
  const port = Number(process.env.E2E_GATEWAY_PORT ?? 8101);
  const { server } = createGatewayStub();
  server.listen(port, () => {
    // eslint-disable-next-line no-console
    console.log(`[gateway-stub] Manjo gateway stub listening on :${port}`);
  });
}
