/**
 * Formats a Date object to 'YYYY-MM-DD' using local calendar date parts.
 *
 * Using `date.toISOString().slice(0, 10)` extracts the UTC calendar date,
 * which causes the date to shift forward or backward for timezones offset from UTC
 * (e.g. UTC-5 where 23:59:59 becomes the next day in UTC).
 */
export function formatLocalDateOnly(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}
