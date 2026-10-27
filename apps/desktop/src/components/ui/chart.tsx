import { useEffect, useMemo, useRef, useState } from "react";
import { fmtTime, isKnown } from "@/lib/format";
import { cn } from "@/lib/utils";

export interface ChartPoint {
  at: string;
  value: number | null;
}

/**
 * Time-series line chart. Null values break the line (a visible gap) instead
 * of being drawn as zero. Pure SVG; sized to its container.
 */
export function LineChart({
  points,
  height = 120,
  color = "var(--color-chart-1)",
  format = (v: number) => String(v),
  className,
  ariaLabel,
  emptyLabel = "No samples yet",
}: {
  points: ChartPoint[];
  height?: number;
  color?: string;
  format?: (v: number) => string;
  className?: string;
  ariaLabel: string;
  emptyLabel?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver((entries) => setWidth(entries[0].contentRect.width));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const pad = { l: 44, r: 8, t: 8, b: 18 };
  const known = points.filter((p) => isKnown(p.value)) as { at: string; value: number }[];

  const geom = useMemo(() => {
    if (known.length === 0 || width <= 0) return null;
    const times = points.map((p) => new Date(p.at).getTime());
    const t0 = Math.min(...times);
    const t1 = Math.max(...times);
    let lo = Math.min(...known.map((p) => p.value));
    let hi = Math.max(...known.map((p) => p.value));
    if (lo === hi) {
      // Centre a flat series so the middle gridline carries its exact value.
      const v = lo;
      const spread = Math.min(Math.abs(v), Math.max(Math.abs(v) * 0.1, Math.abs(v) >= 1 ? 1 : Math.abs(v) * 0.5));
      lo = v === 0 ? 0 : v - spread;
      hi = v === 0 ? 1 : v + spread;
    } else if (lo > 0 && lo / hi < 0.5) {
      lo = 0;
    }
    const w = Math.max(1, width - pad.l - pad.r);
    const h = height - pad.t - pad.b;
    const x = (t: number) => pad.l + (t1 === t0 ? w / 2 : ((t - t0) / (t1 - t0)) * w);
    const y = (v: number) => pad.t + h - ((v - lo) / (hi - lo)) * h;
    const segments: string[] = [];
    let cur = "";
    points.forEach((p, i) => {
      if (!isKnown(p.value)) {
        if (cur) segments.push(cur);
        cur = "";
        return;
      }
      const px = x(times[i]).toFixed(1);
      const py = y(p.value).toFixed(1);
      cur += cur ? ` L${px},${py}` : `M${px},${py}`;
    });
    if (cur) segments.push(cur);
    const gaps: { x1: number; x2: number }[] = [];
    for (let i = 0; i < points.length; i++) {
      if (!isKnown(points[i].value)) {
        const a = x(times[Math.max(0, i - 1)]);
        const b = x(times[Math.min(points.length - 1, i + 1)]);
        gaps.push({ x1: Math.min(a, b), x2: Math.max(a, b) });
      }
    }
    return { x, y, lo, hi, segments, gaps, times, t0, t1, w, h };
  }, [points, known, width, height, pad.l, pad.r, pad.t, pad.b]);

  return (
    <div ref={ref} className={cn("relative w-full select-none", className)} style={{ height }}>
      {!geom ? (
        <div className="flex h-full items-center justify-center rounded-md border border-dashed border-border text-[11px] text-fg-subtle">
          {emptyLabel}
        </div>
      ) : (
        <svg
          width={width}
          height={height}
          role="img"
          aria-label={ariaLabel}
          onMouseLeave={() => setHover(null)}
          onMouseMove={(e) => {
            const rect = (e.currentTarget as SVGSVGElement).getBoundingClientRect();
            const mx = e.clientX - rect.left;
            let best = 0;
            let bestD = Infinity;
            geom.times.forEach((t, i) => {
              const d = Math.abs(geom.x(t) - mx);
              if (d < bestD) {
                bestD = d;
                best = i;
              }
            });
            setHover(best);
          }}
        >
          {[0, 0.5, 1].map((f, i, all) => {
            const v = geom.lo + (geom.hi - geom.lo) * f;
            const yy = geom.y(v);
            const label = format(v);
            const repeated = i > 0 && format(geom.lo + (geom.hi - geom.lo) * all[i - 1]) === label;
            return (
              <g key={f}>
                <line x1={pad.l} x2={width - pad.r} y1={yy} y2={yy} stroke="var(--color-border)" strokeDasharray={f === 0 ? undefined : "2 3"} />
                {!repeated && (
                  <text x={pad.l - 6} y={yy + 3} textAnchor="end" fontSize={10} fill="var(--color-fg-subtle)">
                    {label}
                  </text>
                )}
              </g>
            );
          })}
          {geom.gaps.map((g, i) => (
            <rect key={i} x={g.x1} y={pad.t} width={Math.max(2, g.x2 - g.x1)} height={geom.h} fill="var(--color-warning)" opacity={0.07} />
          ))}
          {geom.segments.map((d, i) => (
            <path key={i} d={d} fill="none" stroke={color} strokeWidth={1.6} strokeLinejoin="round" strokeLinecap="round" />
          ))}
          {geom.segments.length > 0 &&
            points.map((p, i) =>
              isKnown(p.value) && (points.length < 40 || i === points.length - 1) ? (
                <circle key={i} cx={geom.x(geom.times[i])} cy={geom.y(p.value)} r={i === points.length - 1 ? 2.6 : 1.6} fill={color} />
              ) : null,
            )}
          <text x={pad.l} y={height - 4} fontSize={10} fill="var(--color-fg-subtle)">
            {fmtTime(points[0]?.at)}
          </text>
          <text x={width - pad.r} y={height - 4} fontSize={10} textAnchor="end" fill="var(--color-fg-subtle)">
            {fmtTime(points[points.length - 1]?.at)}
          </text>
          {hover !== null && (
            <line x1={geom.x(geom.times[hover])} x2={geom.x(geom.times[hover])} y1={pad.t} y2={pad.t + geom.h} stroke="var(--color-border-strong)" />
          )}
        </svg>
      )}
      {geom && hover !== null && points[hover] && (
        <div
          className="pointer-events-none absolute top-1 z-10 rounded-md border border-border bg-bg-elevated px-2 py-1 text-[11px] shadow-[var(--shadow-overlay)]"
          style={{ left: Math.min(Math.max(geom.x(geom.times[hover]) + 8, 0), Math.max(0, width - 140)) }}
        >
          <div className="text-fg-subtle">{fmtTime(points[hover].at)}</div>
          <div className="tabular font-medium">
            {isKnown(points[hover].value) ? format(points[hover].value as number) : <span className="text-warning">no data</span>}
          </div>
        </div>
      )}
    </div>
  );
}
