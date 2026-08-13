/**
 * Reads the delivered email back off Mailpit.
 *
 * Why this exists: the suite used to assert delivery only through
 * `orders.email_sent`, which says a send succeeded and nothing whatsoever about
 * what was sent. Spec 016's "exactly two attachments" is not observable that way,
 * and Constitution Principle VIII puts a defect reachable only through the
 * assembled system in this tier rather than approximated in a unit test.
 *
 * So these helpers read the real MIME the production SMTPMailer composed, after a
 * real SMTP hop, through the same API a human uses at http://localhost:8025.
 *
 * Mailpit must be running for any spec that calls these. That is a deliberate
 * tightening: playwright.config.ts tolerates a refused SMTP connection, which is
 * fine while nothing inspects the message and useless once something does. A
 * silently absent Mailpit must fail these specs, not pass them.
 */

import { config } from "./env";

/** One attachment as Mailpit reports it. */
export interface MailAttachment {
  PartID: string;
  FileName: string;
  ContentType: string;
  /** Empty for real attachments; set for inline parts the body references as cid:. */
  ContentID: string;
  Size: number;
}

/** A delivered message, with enough of Mailpit's shape for the assertions. */
export interface MailMessage {
  ID: string;
  Subject: string;
  To: Array<{ Address: string }>;
  HTML: string;
  Text: string;
  /**
   * DOCUMENTS only — the receipt and the e-tickets. Mailpit files inline parts
   * separately (verified against the running service), so this stays at 2 even
   * once the body carries a CID logo. That is what lets FR-001's "exactly two
   * document attachments" be asserted literally.
   */
  Attachments: MailAttachment[];
  /** Images the body references by content ID: the brand mark, and the pin. */
  Inline: MailAttachment[];
}

interface MessageSummary {
  ID: string;
  Subject: string;
  To: Array<{ Address: string }>;
  Created: string;
}

async function mailpit<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${config.mailpitURL}${path}`, init);
  if (!response.ok) {
    throw new Error(
      `Mailpit ${init?.method ?? "GET"} ${path} answered ${response.status}. ` +
        `Is it running? \`docker compose up -d mailpit\``,
    );
  }
  return (await response.json()) as T;
}

/**
 * Empties the mailbox.
 *
 * Call this before arranging a delivery. Without it a resend scenario matches the
 * FIRST delivery's message and passes while proving nothing — the failure mode is
 * silent, which is why this is not left to the caller's memory.
 */
export async function clearMailbox(): Promise<void> {
  const response = await fetch(`${config.mailpitURL}/api/v1/messages`, { method: "DELETE" });
  if (!response.ok) {
    throw new Error(`Mailpit could not be cleared: ${response.status}`);
  }
}

/**
 * Waits for a message addressed to `address` and returns it in full.
 *
 * The post-payment chain (ticket issuance, PDF rendering, SMTP) runs in a
 * non-blocking goroutine after the webhook answers 200 — Constitution Principle
 * IV requires exactly that — so the message arrives some time after settlement.
 * Polling is the honest way to observe it.
 *
 * `minCount` waits for at least that many messages to the address, which is how a
 * resend is told apart from the delivery that preceded it.
 */
export async function waitForMail(
  address: string,
  options: { timeoutMs?: number; minCount?: number } = {},
): Promise<MailMessage> {
  const timeoutMs = options.timeoutMs ?? 30_000;
  const minCount = options.minCount ?? 1;
  const deadline = Date.now() + timeoutMs;

  let seen = 0;
  while (Date.now() < deadline) {
    const list = await mailpit<{ messages: MessageSummary[] }>("/api/v1/messages?limit=200");
    const matching = list.messages.filter((m) =>
      m.To.some((to) => to.Address.toLowerCase() === address.toLowerCase()),
    );
    seen = matching.length;
    if (seen >= minCount) {
      // Mailpit lists newest first, so index 0 is the most recent delivery.
      return mailpit<MailMessage>(`/api/v1/message/${matching[0].ID}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }

  throw new Error(
    `No message #${minCount} to ${address} within ${timeoutMs}ms (saw ${seen}). ` +
      `Either delivery failed or Mailpit is not receiving.`,
  );
}

/** Downloads one attachment's bytes. */
export async function downloadAttachment(
  messageID: string,
  partID: string,
): Promise<Buffer> {
  const response = await fetch(`${config.mailpitURL}/api/v1/message/${messageID}/part/${partID}`);
  if (!response.ok) {
    throw new Error(`Mailpit could not return part ${partID}: ${response.status}`);
  }
  return Buffer.from(await response.arrayBuffer());
}

/** Whether the bytes are a PDF at all, rather than an empty or truncated part. */
export function isPDF(content: Buffer): boolean {
  return content.subarray(0, 5).toString("latin1") === "%PDF-";
}

/**
 * Counts pages in a PDF.
 *
 * Page dictionaries are not inside a compressed stream, so this works on the
 * shipped bytes. The page-tree node is `/Type /Pages`, whose text also matches
 * `/Type /Page` — subtracting it is what stops every count coming out one high.
 */
export function pdfPageCount(content: Buffer): number {
  const text = content.toString("latin1");
  const pages = text.split("/Type /Page").length - 1;
  const trees = text.split("/Type /Pages").length - 1;
  return pages - trees;
}

/**
 * Extracts every `cid:` reference from an HTML body.
 *
 * Pairing these against `Inline` is what catches the failure this mechanism is
 * prone to: a reference whose part is missing renders as a broken image and
 * raises no error anywhere — not in the send, not in the SMTP exchange, and not
 * in any assertion that only counts attachments.
 */
export function cidReferences(html: string): string[] {
  const found = new Set<string>();
  for (const match of html.matchAll(/(?:src|background)=["']cid:([^"']+)["']/gi)) {
    found.add(match[1]);
  }
  return [...found];
}
