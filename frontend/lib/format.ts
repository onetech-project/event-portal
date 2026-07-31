const currencyFormatter = new Intl.NumberFormat("id-ID", {
  style: "currency",
  currency: "IDR",
  // Prices are whole rupiah in practice; showing ",00" on every row is noise.
  minimumFractionDigits: 0,
  maximumFractionDigits: 2,
});

const dateTimeFormatter = new Intl.DateTimeFormat("id-ID", {
  dateStyle: "medium",
  timeStyle: "short",
});

/**
 * Formats a decimal money string from the API as rupiah.
 *
 * Amounts arrive as strings ("150000.00") so JSON never rounds them; an
 * unparseable value is echoed back rather than shown as "NaN".
 */
export function formatCurrency(amount: string): string {
  const value = Number(amount);
  if (amount.trim() === "" || Number.isNaN(value)) {
    return amount;
  }
  return currencyFormatter.format(value);
}

/** Formats an RFC3339 timestamp, or a dash when there is none. */
export function formatDateTime(timestamp: string | null | undefined): string {
  if (!timestamp) return "—";

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;

  return dateTimeFormatter.format(date);
}

/**
 * Converts a `<input type="datetime-local">` value to an absolute RFC3339
 * instant. The input has no timezone, so it is interpreted in the admin's local
 * zone — which is what they typed.
 */
export function toApiDateTime(local: string): string {
  return new Date(local).toISOString();
}

/** Converts an RFC3339 instant into a `<input type="datetime-local">` value. */
export function toDateTimeLocal(timestamp: string | null | undefined): string {
  if (!timestamp) return "";

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return "";

  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}
