/**
 * Formatting helpers. `null`/`undefined` always render as an explicit
 * "unknown" marker, never as zero.
 */
export const UNKNOWN = "—";

const intFmt = new Intl.NumberFormat();
const compactFmt = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });

export function isKnown(v: number | null | undefined): v is number {
  return typeof v === "number" && Number.isFinite(v);
}

export function fmtNumber(v: number | null | undefined, opts?: { compact?: boolean; digits?: number }): string {
  if (!isKnown(v)) return UNKNOWN;
  if (opts?.compact && Math.abs(v) >= 10_000) return compactFmt.format(v);
  if (opts?.digits !== undefined) {
    return v.toLocaleString(undefined, { maximumFractionDigits: opts.digits, minimumFractionDigits: 0 });
  }
  return intFmt.format(Math.round(v));
}

export function fmtBytes(v: number | null | undefined): string {
  if (!isKnown(v)) return UNKNOWN;
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let n = Math.abs(v);
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const s = i === 0 ? String(Math.round(n)) : n.toFixed(n < 10 ? 2 : 1);
  return `${v < 0 ? "-" : ""}${s} ${units[i]}`;
}

export function fmtPercent(v: number | null | undefined, digits = 0): string {
  if (!isKnown(v)) return UNKNOWN;
  return `${v.toFixed(digits)}%`;
}

export function fmtRate(v: number | null | undefined, unit = "/s"): string {
  if (!isKnown(v)) return UNKNOWN;
  if (Math.abs(v) >= 100) return `${fmtNumber(v, { compact: true })}${unit}`;
  return `${v.toFixed(v < 10 ? 2 : 1)}${unit}`;
}

export function fmtDurationMs(ms: number | null | undefined): string {
  if (!isKnown(ms)) return UNKNOWN;
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)} s`;
  const m = s / 60;
  if (m < 60) return `${Math.floor(m)}m ${Math.round(s % 60)}s`;
  const h = m / 60;
  if (h < 48) return `${Math.floor(h)}h ${Math.round(m % 60)}m`;
  return `${Math.floor(h / 24)}d ${Math.round(h % 24)}h`;
}

export type TimeZonePref = "local" | "utc";

export function fmtTime(iso: string | null | undefined, tz: TimeZonePref = "local"): string {
  if (!iso) return UNKNOWN;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return UNKNOWN;
  return d.toLocaleTimeString(undefined, { hour12: false, timeZone: tz === "utc" ? "UTC" : undefined });
}

export function fmtDateTime(iso: string | null | undefined, tz: TimeZonePref = "local"): string {
  if (!iso) return UNKNOWN;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return UNKNOWN;
  const s = d.toLocaleString(undefined, {
    hour12: false,
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZone: tz === "utc" ? "UTC" : undefined,
  });
  return tz === "utc" ? `${s} UTC` : s;
}

export function fmtRelative(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "never";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return UNKNOWN;
  const diff = Math.round((now - t) / 1000);
  if (diff < 0) return "just now";
  if (diff < 5) return "just now";
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

export interface SeriesPoint {
  epochId: string;
  at: string;
  value: number | null;
}

/**
 * Per-second rates from a cumulative counter. A decrease is treated as a
 * counter reset (server restart) and produces a gap, as does any unknown
 * sample on either side of an interval.
 */
export function counterRates(points: SeriesPoint[]): SeriesPoint[] {
  const out: SeriesPoint[] = [];
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    const dt = (new Date(b.at).getTime() - new Date(a.at).getTime()) / 1000;
    if (dt <= 0) continue;
    let value: number | null = null;
    if (isKnown(a.value) && isKnown(b.value) && dt > 0 && b.value >= a.value) {
      value = (b.value - a.value) / dt;
    }
    out.push({ epochId: b.epochId, at: b.at, value });
  }
  return out;
}

/** Age in ms past which a snapshot is considered stale for its interval. */
export function staleness(collectedAt: string | null | undefined, intervalMs: number, now = Date.now()) {
  if (!collectedAt) return { stale: true, ageMs: null as number | null };
  const age = now - new Date(collectedAt).getTime();
  return { stale: age > Math.max(intervalMs * 3, 15_000), ageMs: age };
}

export function toCsv(columns: string[], rows: unknown[][]): string {
  const esc = (v: unknown) => {
    if (v === null || v === undefined) return "";
    const s = typeof v === "object" ? JSON.stringify(v) : String(v);
    return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
  };
  return [columns.map(esc).join(","), ...rows.map((r) => r.map(esc).join(","))].join("\n");
}
