import { LineChart } from "@/components/ui/chart";
import { Card } from "@/components/ui/misc";
import { counterRates, fmtBytes, fmtNumber, isKnown } from "@/lib/format";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";

export type MetricKind = "gauge" | "counter";

const COUNTER_METRICS = new Set(["in_msgs", "out_msgs", "in_bytes", "out_bytes", "total_connections", "slow_consumers", "num_redelivered"]);

export function metricKind(name: string): MetricKind {
  return COUNTER_METRICS.has(name.replace(/^js_/, "")) ? "counter" : "gauge";
}

export function formatMetric(unit: string | undefined, v: number, rate = false): string {
  const suffix = rate ? "/s" : "";
  switch (unit) {
    case "bytes":
      return `${fmtBytes(v)}${suffix}`;
    case "percent":
      return `${v.toFixed(1)}%`;
    case "ms":
      return `${v.toFixed(v < 10 ? 2 : 0)} ms`;
    default:
      return `${fmtNumber(v, { compact: v >= 10_000, digits: rate && v < 10 ? 2 : 0 })}${suffix}`;
  }
}

export function MetricChart({
  systemId,
  entityId,
  metric,
  title,
  unit,
  version,
  color,
  limit = 180,
}: {
  systemId: string;
  entityId: string;
  metric: string;
  title: string;
  unit?: string;
  version: number;
  color?: string;
  limit?: number;
}) {
  const kind = metricKind(metric);
  const series = useAsync(() => insightsApi.series(systemId, entityId, metric, limit), [systemId, entityId, metric, limit, version]);
  const raw = series.data ?? [];
  const points = kind === "counter" ? counterRates(raw) : raw;
  const latest = [...points].reverse().find((p) => p.at) ?? null;
  const gaps = points.filter((p) => !isKnown(p.value)).length;

  return (
    <Card className="p-3">
      <div className="mb-1.5 flex items-baseline justify-between gap-2">
        <span className="truncate text-xs font-medium text-fg-muted">
          {title}
          {kind === "counter" && <span className="ml-1 text-fg-subtle">(rate)</span>}
        </span>
        <span className="tabular text-[13px] font-semibold">
          {latest && isKnown(latest.value) ? formatMetric(unit, latest.value, kind === "counter") : <span className="text-fg-subtle">—</span>}
        </span>
      </div>
      {series.error ? (
        <p className="py-8 text-center text-[11px] text-danger">{series.error}</p>
      ) : (
        <LineChart
          points={points}
          height={96}
          color={color}
          ariaLabel={`${title} over time`}
          format={(v) => formatMetric(unit, v, kind === "counter")}
          emptyLabel={kind === "counter" && raw.length === 1 ? "Need two samples to compute a rate" : "No samples yet"}
        />
      )}
      {gaps > 0 && (
        <p className="mt-1 text-[10.5px] text-fg-subtle">
          {gaps} gap{gaps === 1 ? "" : "s"}: sample unavailable{kind === "counter" ? " or counter reset" : ""}.
        </p>
      )}
    </Card>
  );
}
