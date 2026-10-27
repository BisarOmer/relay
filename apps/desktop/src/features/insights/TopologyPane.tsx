import { ChevronRight, Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge, healthTone, severityTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CopyButton, EmptyState, KeyValue, SectionHeader, Spinner } from "@/components/ui/misc";
import { fmtDateTime, fmtRelative, isKnown } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";
import type { PaneProps } from "./InsightsWorkspace";
import { MetricChart, formatMetric, metricKind } from "./MetricChart";
import type { Entity } from "./types";

const TYPE_ORDER = ["system", "cluster", "server", "gateway", "route", "leaf", "account", "stream", "consumer", "connection"];
const TYPE_LABEL: Record<string, string> = {
  system: "System",
  cluster: "Clusters",
  server: "Servers",
  gateway: "Gateways",
  route: "Routes",
  leaf: "Leaf nodes",
  account: "Accounts",
  stream: "Streams",
  consumer: "Consumers",
  connection: "Connections",
};
const CHILD_LIMIT = 200;

function typeRank(t: string) {
  const i = TYPE_ORDER.indexOf(t);
  return i < 0 ? TYPE_ORDER.length : i;
}

function sortEntities(a: Entity, b: Entity) {
  return typeRank(a.type) - typeRank(b.type) || a.displayName.localeCompare(b.displayName);
}

export function TopologyPane({ system, version, focus, navigate }: PaneProps) {
  const topo = useAsync(() => insightsApi.topology(system.id), [system.id, version]);
  const [selected, setSelected] = useState<string | null>(focus.entityId ?? null);
  const [query, setQuery] = useState("");
  const [types, setTypes] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (focus.entityId) setSelected(focus.entityId);
  }, [focus.entityId]);

  const entities = useMemo(() => topo.data?.entities ?? [], [topo.data]);
  const byId = useMemo(() => new Map(entities.map((e) => [e.id, e])), [entities]);
  const children = useMemo(() => {
    const m = new Map<string | null, Entity[]>();
    for (const e of entities) {
      const parent = e.parentId && byId.has(e.parentId) ? e.parentId : null;
      if (!m.has(parent)) m.set(parent, []);
      m.get(parent)!.push(e);
    }
    for (const list of m.values()) list.sort(sortEntities);
    return m;
  }, [entities, byId]);
  const typeCounts = useMemo(() => {
    const c = new Map<string, number>();
    for (const e of entities) c.set(e.type, (c.get(e.type) ?? 0) + 1);
    return [...c.entries()].sort((a, b) => typeRank(a[0]) - typeRank(b[0]));
  }, [entities]);

  // Expand the path to the selected entity and the top two levels by default.
  useEffect(() => {
    if (entities.length === 0) return;
    setExpanded((prev) => {
      const next = new Set(prev);
      if (prev.size === 0) {
        for (const r of children.get(null) ?? []) {
          next.add(r.id);
          for (const c of children.get(r.id) ?? []) if (c.type !== "server" || (children.get(r.id)?.length ?? 0) < 6) next.add(c.id);
        }
      }
      let cur = selected ? byId.get(selected) : undefined;
      while (cur?.parentId && byId.has(cur.parentId)) {
        next.add(cur.parentId);
        cur = byId.get(cur.parentId);
      }
      return next;
    });
  }, [entities, children, byId, selected]);

  const q = query.trim().toLowerCase();
  const filtering = q !== "" || types.size > 0;
  const matches = useMemo(() => {
    if (!filtering) return [];
    return entities
      .filter((e) => (types.size === 0 || types.has(e.type)) && (!q || e.displayName.toLowerCase().includes(q) || e.id.toLowerCase().includes(q)))
      .sort(sortEntities);
  }, [entities, filtering, q, types]);

  if (!topo.data) {
    if (topo.error) return <EmptyState title="Could not load topology" description={topo.error} action={<Button size="sm" onClick={topo.reload}>Retry</Button>} />;
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading topology…" />
      </div>
    );
  }
  if (entities.length === 0) {
    return <EmptyState title="No topology yet" description="Collect a snapshot to see servers, connections and streams." />;
  }

  const toggle = (id: string) =>
    setExpanded((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  return (
    <div className="grid h-full min-h-0 grid-cols-[minmax(280px,34%)_1fr]">
      <div className="flex min-h-0 flex-col border-r border-border">
        <div className="space-y-2 border-b border-border p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-subtle" />
            <input
              aria-label="Search entities"
              placeholder="Search by name or ID"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="h-7 w-full rounded-md border border-border bg-bg-inset pl-7 pr-2 text-xs placeholder:text-fg-subtle focus:border-accent focus:outline-none"
            />
          </div>
          <div className="flex flex-wrap gap-1" role="group" aria-label="Filter by type">
            {typeCounts.map(([t, n]) => {
              const on = types.has(t);
              return (
                <button
                  key={t}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    setTypes((s) => {
                      const next = new Set(s);
                      if (on) next.delete(t);
                      else next.add(t);
                      return next;
                    })
                  }
                  className={cn(
                    "h-6 rounded-md border px-2 text-[11px]",
                    on ? "border-accent/60 bg-accent/15 text-fg" : "border-border text-fg-muted hover:bg-bg-hover",
                  )}
                >
                  {TYPE_LABEL[t] ?? t} <span className="tabular text-fg-subtle">{n}</span>
                </button>
              );
            })}
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-1" role="tree" aria-label="Topology">
          {filtering ? (
            matches.length === 0 ? (
              <p className="p-3 text-xs text-fg-subtle">No entities match.</p>
            ) : (
              matches.slice(0, 500).map((e) => <Row key={e.id} e={e} depth={0} selected={selected === e.id} onSelect={setSelected} showType />)
            )
          ) : (
            <Tree nodes={children.get(null) ?? []} depth={0} childrenOf={children} expanded={expanded} toggle={toggle} selected={selected} onSelect={setSelected} />
          )}
          {filtering && matches.length > 500 && <p className="p-2 text-[11px] text-fg-subtle">Showing 500 of {matches.length}. Refine the search.</p>}
        </div>
      </div>
      <div className="min-h-0 overflow-auto">
        {selected ? (
          <EntityPanel systemId={system.id} entityId={selected} version={version} onSelect={setSelected} onFinding={(id) => navigate("findings", { findingId: id })} />
        ) : (
          <EmptyState title="Select an entity" description="Choose a server, connection, stream or other entity to see its details and history." />
        )}
      </div>
    </div>
  );
}

function Tree({
  nodes,
  depth,
  childrenOf,
  expanded,
  toggle,
  selected,
  onSelect,
}: {
  nodes: Entity[];
  depth: number;
  childrenOf: Map<string | null, Entity[]>;
  expanded: Set<string>;
  toggle: (id: string) => void;
  selected: string | null;
  onSelect: (id: string) => void;
}) {
  const [limit, setLimit] = useState(CHILD_LIMIT);
  return (
    <>
      {nodes.slice(0, limit).map((e) => {
        const kids = childrenOf.get(e.id) ?? [];
        const open = expanded.has(e.id);
        return (
          <div key={e.id} role="treeitem" aria-expanded={kids.length ? open : undefined} aria-selected={selected === e.id}>
            <Row e={e} depth={depth} selected={selected === e.id} onSelect={onSelect} hasKids={kids.length > 0} open={open} onToggle={() => toggle(e.id)} count={kids.length} />
            {open && kids.length > 0 && (
              <div role="group">
                <Tree nodes={kids} depth={depth + 1} childrenOf={childrenOf} expanded={expanded} toggle={toggle} selected={selected} onSelect={onSelect} />
              </div>
            )}
          </div>
        );
      })}
      {nodes.length > limit && (
        <button type="button" className="ml-6 py-1 text-[11px] text-accent hover:underline" style={{ paddingLeft: depth * 14 }} onClick={() => setLimit((l) => l + CHILD_LIMIT)}>
          Show {Math.min(CHILD_LIMIT, nodes.length - limit)} more of {nodes.length - limit}
        </button>
      )}
    </>
  );
}

function Row({
  e,
  depth,
  selected,
  onSelect,
  hasKids,
  open,
  onToggle,
  count,
  showType,
}: {
  e: Entity;
  depth: number;
  selected: boolean;
  onSelect: (id: string) => void;
  hasKids?: boolean;
  open?: boolean;
  onToggle?: () => void;
  count?: number;
  showType?: boolean;
}) {
  return (
    <div className={cn("group flex h-7 items-center rounded-md pr-2 text-xs", selected ? "bg-bg-active" : "hover:bg-bg-hover")} style={{ paddingLeft: depth * 14 }}>
      <button
        type="button"
        tabIndex={-1}
        aria-hidden={!hasKids}
        className={cn("flex h-6 w-5 shrink-0 items-center justify-center text-fg-subtle", !hasKids && "invisible")}
        onClick={onToggle}
      >
        <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
      </button>
      <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => onSelect(e.id)} title={e.id}>
        <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", toneDot(e.status))} aria-label={e.status} role="img" />
        <span className="w-[5.5rem] shrink-0 truncate text-[10.5px] uppercase tracking-wide text-fg-subtle">{e.type}</span>
        <span className={cn("truncate", e.freshness !== "fresh" && "text-fg-subtle")}>{e.displayName}</span>
        {showType === undefined && hasKids && <span className="tabular ml-auto shrink-0 text-[10.5px] text-fg-subtle">{count}</span>}
      </button>
    </div>
  );
}

function toneDot(status: string) {
  const t = healthTone(status);
  return t === "success" ? "bg-success" : t === "warning" ? "bg-warning" : t === "danger" ? "bg-danger" : "bg-fg-subtle";
}

const HIDDEN_PROPS = new Set(["demo"]);

function propValue(v: unknown): React.ReactNode {
  if (v === null || v === undefined || v === "") return undefined;
  if (typeof v === "boolean") return v ? "yes" : "no";
  if (Array.isArray(v)) return v.length ? v.join(", ") : "none";
  if (typeof v === "object") return <span className="font-mono text-[11px]">{JSON.stringify(v)}</span>;
  return String(v);
}

function EntityPanel({
  systemId,
  entityId,
  version,
  onSelect,
  onFinding,
}: {
  systemId: string;
  entityId: string;
  version: number;
  onSelect: (id: string) => void;
  onFinding: (id: string) => void;
}) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const detail = useAsync(() => insightsApi.entity(systemId, entityId), [systemId, entityId, version]);
  const [chartMetric, setChartMetric] = useState<string | null>(null);

  useEffect(() => setChartMetric(null), [entityId]);

  if (!detail.data || detail.data.entity.id !== entityId) {
    if (detail.error)
      return (
        <EmptyState
          title="Entity not in the latest snapshot"
          description={`${detail.error}. It may have been removed, or the endpoint that reports it failed.`}
        />
      );
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading…" />
      </div>
    );
  }
  const d = detail.data;
  const e = d.entity;
  const metrics = [...d.metrics].sort((a, b) => a.name.localeCompare(b.name));
  const chartable = metrics.filter((m) => m.available);
  const active = chartMetric ?? chartable[0]?.name ?? null;
  const activeUnit = metrics.find((m) => m.name === active)?.unit;
  const props = Object.entries(e.props).filter(([k]) => !HIDDEN_PROPS.has(k));

  return (
    <div className="space-y-4 p-5">
      <nav aria-label="Entity path" className="flex flex-wrap items-center gap-1 text-[11px] text-fg-subtle">
        {d.path.map((p) => (
          <span key={p.id} className="flex items-center gap-1">
            <button type="button" className="hover:text-fg" onClick={() => onSelect(p.id)}>
              {p.displayName}
            </button>
            <ChevronRight className="h-3 w-3" />
          </span>
        ))}
        <span className="text-fg-muted">{e.displayName}</span>
      </nav>
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-[15px] font-semibold">{e.displayName}</h2>
            <Badge>{e.type}</Badge>
            <Badge tone={healthTone(e.status)} dot>
              {e.status}
            </Badge>
            {e.freshness !== "fresh" && (
              <Badge tone="warning" title={e.freshness === "partial" ? "Some monitoring endpoints were unavailable in this snapshot" : "Not reported in the latest snapshot"}>
                {e.freshness === "partial" ? "partial data" : e.freshness}
              </Badge>
            )}
            {e.props.demo === true && <Badge tone="warning">simulated</Badge>}
          </div>
          <div className="mt-1 flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
            {e.id}
            <CopyButton text={e.id} label="Copy entity ID" />
          </div>
        </div>
        <div className="text-right text-[11px] text-fg-subtle">
          <div>Last seen {fmtRelative(e.lastSeenAt)}</div>
          {e.firstSeenAt && <div title={fmtDateTime(e.firstSeenAt, tz)}>First seen {fmtRelative(e.firstSeenAt)}</div>}
        </div>
      </div>

      {d.findings.length > 0 && (
        <Card className="p-3">
          <SectionHeader title="Findings" className="mb-2" />
          <ul className="space-y-1">
            {d.findings.map((f) => (
              <li key={f.id}>
                <button type="button" className="flex w-full items-center gap-2 rounded px-1 py-1 text-left text-xs hover:bg-bg-hover" onClick={() => onFinding(f.id)}>
                  <Badge tone={severityTone(f.severity)}>{f.severity}</Badge>
                  <span className="truncate">{f.title}</span>
                  <span className="ml-auto text-[11px] text-fg-subtle">{f.status}</span>
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {active && (
        <div className="space-y-2">
          <div className="flex flex-wrap gap-1" role="group" aria-label="Chart metric">
            {chartable.map((m) => (
              <button
                key={m.name}
                type="button"
                aria-pressed={active === m.name}
                onClick={() => setChartMetric(m.name)}
                className={cn("h-6 rounded-md border px-2 font-mono text-[10.5px]", active === m.name ? "border-accent/60 bg-accent/15" : "border-border text-fg-muted hover:bg-bg-hover")}
              >
                {m.name}
              </button>
            ))}
          </div>
          <MetricChart systemId={systemId} entityId={e.id} metric={active} title={active} unit={activeUnit} version={version} />
        </div>
      )}

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="p-3">
          <SectionHeader title="Metrics" description={`Snapshot ${fmtDateTime(d.collectedAt, tz)}`} className="mb-2" />
          {metrics.length === 0 ? (
            <p className="text-xs text-fg-subtle">No metrics for this entity.</p>
          ) : (
            <table className="w-full text-xs">
              <tbody>
                {metrics.map((m) => (
                  <tr key={m.name} className="border-t border-border first:border-t-0">
                    <td className="py-1 pr-2 font-mono text-fg-muted">{m.name}</td>
                    <td className="tabular py-1 text-right">
                      {m.available && isKnown(m.value) ? formatMetric(m.unit, m.value) : <span className="text-fg-subtle" title="Not reported in this snapshot">unavailable</span>}
                    </td>
                    <td className="w-14 py-1 pl-2 text-right text-[10.5px] text-fg-subtle">{metricKind(m.name) === "counter" ? "counter" : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
        <Card className="p-3">
          <SectionHeader title="Properties" className="mb-2" />
          {props.length === 0 ? <p className="text-xs text-fg-subtle">None reported.</p> : <KeyValue items={props.map(([k, v]) => [<span className="font-mono">{k}</span>, propValue(v)])} />}
        </Card>
      </div>

      {d.related.length > 0 && (
        <Card className="p-3">
          <SectionHeader title="Related" className="mb-2" />
          <ul className="flex flex-wrap gap-1.5">
            {d.related.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  disabled={!r.inSnapshot}
                  title={r.inSnapshot ? r.id : `${r.id} is not in the latest snapshot`}
                  onClick={() => onSelect(r.id)}
                  className="inline-flex h-6 items-center gap-1.5 rounded-md border border-border px-2 text-[11px] hover:bg-bg-hover disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <span className="uppercase text-fg-subtle">{r.type ?? "?"}</span>
                  {r.displayName ?? r.id}
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {d.children.length > 0 && (
        <Card className="p-3">
          <SectionHeader title={`Children (${d.children.length})`} className="mb-2" />
          <ul className="max-h-64 overflow-auto">
            {[...d.children].sort(sortEntities).slice(0, 300).map((c) => (
              <li key={c.id}>
                <button type="button" className="flex w-full items-center gap-2 rounded px-1 py-1 text-left text-xs hover:bg-bg-hover" onClick={() => onSelect(c.id)}>
                  <span className={cn("h-1.5 w-1.5 rounded-full", toneDot(c.status))} />
                  <span className="w-16 text-[10.5px] uppercase text-fg-subtle">{c.type}</span>
                  <span className="truncate">{c.displayName}</span>
                </button>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  );
}
