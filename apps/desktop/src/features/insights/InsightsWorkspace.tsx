import { AlertTriangle, Database, GitCompare, LayoutDashboard, Network, Play, Radar, Server, Settings2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge, healthTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/input";
import { Banner, EmptyState, Spinner } from "@/components/ui/misc";
import { errorMessage } from "@/lib/engine";
import { fmtDurationMs, fmtRelative, staleness } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { toast } from "@/stores/ui-store";
import { insightsApi } from "./api";
import { FindingsPane } from "./FindingsPane";
import { HistoryPane } from "./HistoryPane";
import { useAsync, useNow } from "./hooks";
import { OverviewPane } from "./OverviewPane";
import { QueryPane } from "./QueryPane";
import { SystemsPane } from "./SystemsPane";
import { TopologyPane } from "./TopologyPane";
import type { InsightsSystem } from "./types";

export type InsightsView = "overview" | "topology" | "findings" | "history" | "query" | "systems";

const VIEWS: { id: InsightsView; label: string; icon: typeof Radar }[] = [
  { id: "overview", label: "Overview", icon: LayoutDashboard },
  { id: "topology", label: "Topology", icon: Network },
  { id: "findings", label: "Findings", icon: AlertTriangle },
  { id: "history", label: "History", icon: GitCompare },
  { id: "query", label: "Query", icon: Database },
  { id: "systems", label: "Systems", icon: Settings2 },
];

export interface PaneProps {
  system: InsightsSystem;
  /** Increments whenever a new epoch for this system is stored. */
  version: number;
  navigate: (view: InsightsView, focus?: { entityId?: string; findingId?: string }) => void;
  focus: { entityId?: string; findingId?: string };
}

export function InsightsWorkspace() {
  const engineReady = useAppStore((s) => s.engineState === "ready");
  const subscribeEvents = useAppStore((s) => s.subscribeEvents);
  const setCrumbs = useAppStore((s) => s.setCrumbs);
  const [view, setView] = useState<InsightsView>("overview");
  const [focus, setFocus] = useState<PaneProps["focus"]>({});
  const [systemId, setSystemId] = useState<string | null>(null);
  const [versions, setVersions] = useState<Record<string, number>>({});
  const [liveStatus, setLiveStatus] = useState<Record<string, { status: string; error?: string }>>({});
  const [collecting, setCollecting] = useState(false);
  const now = useNow(5000);

  const systems = useAsync(() => insightsApi.listSystems(), [], engineReady);
  const list = useMemo(() => systems.data ?? [], [systems.data]);
  const system = list.find((s) => s.id === systemId) ?? list[0];

  useEffect(() => {
    if (!systemId && list[0]) setSystemId(list[0].id);
  }, [list, systemId]);

  const reloadSystems = systems.reload;
  useEffect(() => {
    return subscribeEvents((ev) => {
      if (ev.event === "insights.epoch") {
        const p = ev.payload as { systemId: string; status: string; error?: string };
        setVersions((v) => ({ ...v, [p.systemId]: (v[p.systemId] ?? 0) + 1 }));
        setLiveStatus((s) => ({ ...s, [p.systemId]: { status: p.status, error: p.error } }));
        reloadSystems();
      } else if (ev.event === "insights.status") {
        const p = ev.payload as { systemId: string; status: string; error?: string };
        setLiveStatus((s) => ({ ...s, [p.systemId]: { status: p.status, error: p.error } }));
        if (p.status !== "collecting") reloadSystems();
      }
    });
  }, [subscribeEvents, reloadSystems]);

  const viewLabel = VIEWS.find((v) => v.id === view)?.label ?? "";
  useEffect(() => {
    setCrumbs(system && view !== "systems" ? [system.name, viewLabel] : [viewLabel]);
  }, [system, view, viewLabel, setCrumbs]);

  const navigate: PaneProps["navigate"] = (v, f) => {
    setFocus(f ?? {});
    setView(v);
  };

  async function collectNow() {
    if (!system) return;
    setCollecting(true);
    try {
      const r = await insightsApi.collect(system.id);
      if (r.status === "unreachable" || r.status === "error") toast.error("Collection failed", r.error ?? r.status);
      else if (r.status === "partial") toast.warning("Partial collection", r.error ?? "Some endpoints were unavailable.");
    } catch (e) {
      toast.error("Collection failed", errorMessage(e));
    } finally {
      setCollecting(false);
    }
  }

  if (!engineReady) {
    return <EmptyState icon={Radar} title="Engine not ready" description="Insights needs the Relay engine." />;
  }
  if (systems.loading && !systems.data) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading monitored systems…" />
      </div>
    );
  }
  if (systems.error && !systems.data) {
    return <EmptyState title="Could not load systems" description={systems.error} action={<Button size="sm" onClick={systems.reload}>Retry</Button>} />;
  }

  const status = system ? (liveStatus[system.id]?.status ?? system.lastStatus) : undefined;
  const isCollecting = collecting || status === "collecting";
  const stale = system?.lastSuccessAt ? staleness(system.lastSuccessAt, system.collectIntervalMs, now) : null;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-11 shrink-0 items-center gap-3 border-b border-border bg-bg-panel px-4">
        {list.length > 0 && view !== "systems" && (
          <>
            <Select aria-label="Monitored system" className="h-7 w-56 text-xs" value={system?.id ?? ""} onChange={(e) => setSystemId(e.target.value)}>
              {list.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                  {s.demo ? " (demo data)" : ""}
                  {!s.enabled ? " — paused" : ""}
                </option>
              ))}
            </Select>
            {system && (
              <div className="flex min-w-0 items-center gap-2 text-[11px] text-fg-subtle">
                <Badge tone={isCollecting ? "info" : healthTone(status)} dot>
                  {isCollecting ? "collecting" : status}
                </Badge>
                {system.demo && <Badge tone="warning">simulated</Badge>}
                <span className="truncate" title={system.lastSuccessAt ?? undefined}>
                  {system.lastSuccessAt
                    ? `Updated ${fmtRelative(system.lastSuccessAt, now)}`
                    : system.lastCollectedAt
                      ? "No successful collection yet"
                      : "Never collected"}
                </span>
                {system.enabled ? (
                  <span className="hidden lg:inline">· every {fmtDurationMs(system.collectIntervalMs)}</span>
                ) : (
                  <span className="text-warning">· automatic collection paused</span>
                )}
              </div>
            )}
          </>
        )}
        <div className="ml-auto flex items-center gap-2">
          {system && view !== "systems" && (
            <Button size="sm" variant="outline" loading={isCollecting} disabled={isCollecting} onClick={() => void collectNow()}>
              <Play className="h-3 w-3" /> Collect now
            </Button>
          )}
        </div>
      </div>
      <div className="flex min-h-0 flex-1">
        <nav aria-label="Insights" className="w-11 shrink-0 space-y-0.5 border-r border-border bg-bg-panel p-1.5 xl:w-44">
          {VIEWS.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              type="button"
              aria-current={view === id ? "page" : undefined}
              aria-label={label}
              title={label}
              disabled={!system && id !== "systems"}
              onClick={() => navigate(id)}
              className={cn(
                "flex h-8 w-full items-center justify-center gap-2 rounded-md px-2.5 text-[13px] disabled:opacity-40 xl:justify-start",
                view === id ? "bg-bg-active text-fg" : "text-fg-muted hover:bg-bg-hover hover:text-fg",
              )}
            >
              <Icon className={cn("h-3.5 w-3.5 shrink-0", view === id && "text-accent")} />
              <span className="hidden xl:inline">{label}</span>
            </button>
          ))}
        </nav>
        <div className="flex min-w-0 flex-1 flex-col">
          {system && view !== "systems" && stale?.stale && system.enabled && !isCollecting && (
            <Banner tone="warning" className="border-x-0 border-t-0">
              Data is stale: the last successful collection was {fmtRelative(system.lastSuccessAt, now)}, but the interval is{" "}
              {fmtDurationMs(system.collectIntervalMs)}.
              {system.lastError ? ` Last error: ${system.lastError}` : ""}
            </Banner>
          )}
          {system && view !== "systems" && status === "unreachable" && !stale?.stale && (
            <Banner tone="danger" className="border-x-0 border-t-0">
              The monitoring endpoint is unreachable{system.lastError ? `: ${system.lastError}` : ""}.{" "}
              {system.lastSuccessAt
                ? `Last successful snapshot ${fmtRelative(system.lastSuccessAt, now)}; use History to compare it.`
                : "No snapshot has succeeded yet."}
            </Banner>
          )}
          <div className="min-h-0 flex-1 overflow-hidden">
            {!system || view === "systems" ? (
              <SystemsPane
                systems={list}
                onChanged={(id) => {
                  systems.reload();
                  if (id) {
                    setSystemId(id);
                    setView("overview");
                  }
                }}
                onDeleted={(id) => {
                  if (id === systemId) setSystemId(null);
                  systems.reload();
                }}
              />
            ) : (
              <PaneSwitch view={view} system={system} version={versions[system.id] ?? 0} navigate={navigate} focus={focus} />
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function PaneSwitch({ view, ...props }: PaneProps & { view: InsightsView }) {
  switch (view) {
    case "overview":
      return <OverviewPane {...props} />;
    case "topology":
      return <TopologyPane key={props.system.id} {...props} />;
    case "findings":
      return <FindingsPane key={props.system.id} {...props} />;
    case "history":
      return <HistoryPane key={props.system.id} {...props} />;
    case "query":
      return <QueryPane key={props.system.id} {...props} />;
    default:
      return <EmptyState icon={Server} title="Unknown view" />;
  }
}
