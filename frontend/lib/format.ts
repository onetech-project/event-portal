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

/**
 * Formats an event's date range the way the detail info bar shows it
 * (Figma 4-5): "26 - 27 Sept 2026". Shared parts are not repeated — same
 * month collapses to one month/year, same day to a single date; ranges that
 * cross a month or year spell both sides out.
 */
export function formatDateRange(start: string, end: string): string {
  const from = new Date(start);
  const to = new Date(end);
  if (Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) {
    return `${formatDateTime(start)} - ${formatDateTime(end)}`;
  }

  const day = new Intl.DateTimeFormat("id-ID", { day: "numeric" });
  const dayMonth = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short" });
  const full = new Intl.DateTimeFormat("id-ID", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });

  const sameDay =
    from.getFullYear() === to.getFullYear() &&
    from.getMonth() === to.getMonth() &&
    from.getDate() === to.getDate();
  if (sameDay) return full.format(from);

  if (from.getFullYear() === to.getFullYear() && from.getMonth() === to.getMonth()) {
    return `${day.format(from)} - ${full.format(to)}`;
  }
  if (from.getFullYear() === to.getFullYear()) {
    return `${dayMonth.format(from)} - ${full.format(to)}`;
  }
  return `${full.format(from)} - ${full.format(to)}`;
}

/**
 * Formats the expected visitor count for the info bar's Scale cell: dot-grouped
 * below one million ("100.000"), compact above ("1M", "2,5M", "1B", "1T").
 */
export function formatVisitorCount(count: number): string {
  const units: Array<[number, string]> = [
    [1_000_000_000_000, "T"],
    [1_000_000_000, "B"],
    [1_000_000, "M"],
  ];
  for (const [size, suffix] of units) {
    if (count >= size) {
      const scaled = count / size;
      // One decimal at most, and no trailing ",0" — 1500000 reads "1,5M",
      // 1000000 reads "1M".
      const rounded = Math.round(scaled * 10) / 10;
      return (
        new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1 }).format(rounded) + suffix
      );
    }
  }
  return new Intl.NumberFormat("id-ID").format(count);
}

/** Formats an RFC3339 timestamp, or a dash when there is none. */
export function formatDateTime(timestamp: string | null | undefined): string {
  if (!timestamp) return "—";

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;

  return dateTimeFormatter.format(date);
}

export function formatDate(timestamp: string | null | undefined): string {
  if (!timestamp) return "—";

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return timestamp;

  return new Intl.DateTimeFormat("id-ID", { dateStyle: "medium" }).format(date);
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
