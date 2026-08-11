/**
 * A stand-in for the Midtrans Core API, so the purchase flow can be exercised
 * end to end without the network, without sandbox credentials, and without a
 * human scanning a QR code.
 *
 * It implements exactly the two endpoints `internal/payment/midtrans.go` calls:
 *
 *   POST /v2/charge          — opens a QRIS session
 *   GET  /v2/:order_id/status — the reconciliation read
 *
 * and nothing else. The shapes below are dictated by `chargeResponse` and
 * `statusResponse` in that file; in particular `qr_string` must be non-empty
 * (checkout fails deliberately without it) and `expiry_time` must parse as
 * "YYYY-MM-DD HH:MM:SS" in Asia/Jakarta.
 *
 * Run standalone: `node --experimental-strip-types support/gateway-stub.ts`
 */

import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

/** What the stub remembers about each order, so /status can answer consistently. */
type StubTransaction = {
  transactionId: string;
  orderId: string;
  grossAmount: string;
  status: string;
};

const transactions = new Map<string, StubTransaction>();

/** Midtrans reports timestamps in WIB (UTC+7) as "YYYY-MM-DD HH:MM:SS". */
function jakartaTimestamp(offsetMinutes: number): string {
  const wib = new Date(Date.now() + offsetMinutes * 60_000 + 7 * 60 * 60_000);
  return wib.toISOString().replace("T", " ").slice(0, 19);
}

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
 * Lets a test force the next charge to fail, so the compensating path — the
 * guest keeps their hold and their saved forms — can be exercised too.
 */
let failNextCharge = false;

export function createGatewayStub() {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? "/", "http://stub");

    // Test-only control surface. Namespaced under /__stub so it can never
    // collide with a real provider path.
    if (url.pathname === "/__stub/fail-next-charge" && req.method === "POST") {
      failNextCharge = true;
      return json(res, 200, { ok: true });
    }
    if (url.pathname === "/__stub/health") {
      return json(res, 200, { ok: true, transactions: transactions.size });
    }
    if (url.pathname === "/__stub/reset" && req.method === "POST") {
      transactions.clear();
      failNextCharge = false;
      return json(res, 200, { ok: true });
    }

    if (url.pathname === "/v2/charge" && req.method === "POST") {
      if (failNextCharge) {
        failNextCharge = false;
        return json(res, 500, {
          status_code: "500",
          status_message: "stub: forced failure",
          validation_messages: ["forced by the e2e stub"],
        });
      }

      const body = JSON.parse((await readBody(req)) || "{}");
      const orderId: string = body?.transaction_details?.order_id ?? "unknown";
      const grossAmount: string = String(body?.transaction_details?.gross_amount ?? "0");
      const transactionId = `stub-tx-${orderId}`;

      transactions.set(orderId, {
        transactionId,
        orderId,
        grossAmount,
        status: "pending",
      });

      // The expiry must sit far enough out that the API's own shorter payment
      // window is always the binding deadline — that is the invariant config
      // enforces, and the stub must not be the thing that breaks it.
      return json(res, 200, {
        status_code: "201",
        status_message: "QRIS transaction is created",
        transaction_id: transactionId,
        order_id: orderId,
        gross_amount: grossAmount,
        transaction_status: "pending",
        fraud_status: "accept",
        payment_type: "qris",
        // A plausible QRIS payload. Only its non-emptiness is load-bearing:
        // the API renders the PNG itself from this string.
        qr_string: `00020101021226610014COM.STUB.WWW0118${orderId}5204599953033605802ID6304ABCD`,
        expiry_time: jakartaTimestamp(30),
        actions: [
          {
            name: "generate-qr-code",
            method: "GET",
            url: `http://localhost:8101/v2/qr/${encodeURIComponent(orderId)}`,
          },
        ],
      });
    }

    // GET /v2/:order_id/status
    const statusMatch = url.pathname.match(/^\/v2\/(.+)\/status$/);
    if (statusMatch && req.method === "GET") {
      const orderId = decodeURIComponent(statusMatch[1]);
      const tx = transactions.get(orderId);

      if (!tx) {
        // Midtrans answers an unknown transaction with HTTP 200 and "404" in the
        // body — the exact trap `statusResponse` documents. Reproducing it keeps
        // the reconciliation path honest.
        return json(res, 200, {
          status_code: "404",
          status_message: "Transaction doesn't exist.",
          order_id: orderId,
        });
      }

      return json(res, 200, {
        status_code: tx.status === "settlement" ? "200" : "201",
        status_message: "Success",
        transaction_id: tx.transactionId,
        order_id: tx.orderId,
        gross_amount: tx.grossAmount,
        transaction_status: tx.status,
        fraud_status: "accept",
        payment_type: "qris",
      });
    }

    // The provider-hosted QR image. Never fetched by the API — it generates its
    // own PNG — but a 404 here would be a confusing red herring in a trace.
    if (url.pathname.startsWith("/v2/qr/")) {
      res.writeHead(200, { "content-type": "image/png" });
      return res.end(Buffer.from([0x89, 0x50, 0x4e, 0x47]));
    }

    json(res, 404, { status_code: "404", status_message: "stub: no such path" });
  });

  return {
    server,
    /** Marks an order settled, so a later reconciliation read agrees with the webhook. */
    markSettled(orderId: string): void {
      const tx = transactions.get(orderId);
      if (tx) tx.status = "settlement";
    },
    grossAmountFor(orderId: string): string | undefined {
      return transactions.get(orderId)?.grossAmount;
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
    console.log(`[gateway-stub] Midtrans Core API stub listening on :${port}`);
  });
}
