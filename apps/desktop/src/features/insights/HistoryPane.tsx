import { ArrowRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Select } from "@/components/ui/input";
import { Banner, EmptyState, SectionHeader, Spinner } from "@/components/ui/misc";
import { fmtDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";
import type { PaneProps } from "./InsightsWorkspace";
import type { DiffChange } from "./types";

const KIND_LABEL: Record<DiffChange["kind"], string> = {
  added: "Added",
  removed: "Removed",
  missing_or_removed: "Missing (data incomplete)",
  changed: "Changed",
};
const KIND_TONE = { added: "success", removed: "danger", missing_or_removed: "warning", changed: "info" } as const;

function show(v: unknown) {
  if (v === undefined || v === null) return "—";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

export function HistoryPane({ system, version, navigate }: PaneProps) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const epochs = useAsync(() => insightsApi.epochs(system.id, 500), [system.id, version]);
  const list = useMemo(() => epochs.data ?? [], [epochs.data]);
  const [from, setFrom] = useState<string>("");
  const [to, setTo] = useState<string>("");
  const [kindFilter, setKindFilter] = useState<Set<DiffChange["kind"]>>(new Set());

  useEffect(() => {
    if (list.length >= 2 && (!from || !list.some((e) => e.id === from))) {
      setTo(list[0].id);
      setFrom(list[1].id);
    }
  }, [list, from]);

  const diff = useAsync(() => insightsApi.diff(system.id, from, to), [system.id, from, to], Boolean(from && to && from !== to));

  if (!epochs.data) {
    if (epochs.error) return <EmptyState title="Could not load history" description={epochs.error} />;
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading snapshots…" />
      </div>
    );
  }
  if (list.length < 2) {
    return <EmptyState title="Not enough history" description="Two snapshots are needed to compare. Collect again to see what changed." />;
  }

  const option = (e: (typeof list)[number]) =>
    `${fmtDateTime(e.collectedAt, tz)} · ${Math.round(e.completeness * 100)}%${e.errorCount ? ` · ${e.errorCount} errors` : ""}`;

  const changes = (diff.data?.changes ?? []).filter((c) => kindFilter.size === 0 || kindFilter.has(c.kind));
  const counts = (diff.data?.changes ?? []).reduce<Record<string, number>>((acc, c) => ({ ...acc, [c.kind]: (acc[c.kind] ?? 0) + 1 }), {});

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
        <SectionHeader title="Compare snapshots" />
        <Select aria-label="From snapshot" className="h-7 w-auto text-xs" value={from} onChange={(e) => setFrom(e.target.value)}>
          {list.map((e) => (
            <option key={e.id} value={e.id}>
              {option(e)}
            </option>
          ))}
        </Select>
        <ArrowRight className="h-3.5 w-3.5 text-fg-subtle" />
        <Select aria-label="To snapshot" className="h-7 w-auto text-xs" value={to} onChange={(e) => setTo(e.target.value)}>
          {list.map((e) => (
            <option key={e.id} value={e.id}>
              {option(e)}
            </option>
          ))}
        </Select>
        <span className="ml-auto text-[11px] text-fg-subtle">{list.length} snapshots retained</span>
      </div>
      {from === to && <p className="p-4 text-xs text-fg-subtle">Choose two different snapshots.</p>}
      {diff.loading && !diff.data && (
        <div className="p-4">
          <Spinner label="Comparing…" />
        </div>
      )}
      {diff.error && <p className="p-4 text-xs text-danger">{diff.error}</p>}
      {diff.data && from !== to && (
        <>
          {diff.data.incomplete && (
            <Banner tone="warning" className="border-x-0 border-t-0">
              One of the snapshots is incomplete. Entities missing from it may not really have been removed.
            </Banner>
          )}
          <div className="flex flex-wrap gap-1 border-b border-border px-4 py-2" role="group" aria-label="Filter changes">
            {(Object.keys(KIND_LABEL) as DiffChange["kind"][])
              .filter((k) => counts[k])
              .map((k) => {
                const on = kindFilter.has(k);
                return (
                  <button
                    key={k}
                    type="button"
                    aria-pressed={on}
                    onClick={() =>
                      setKindFilter((s) => {
                        const n = new Set(s);
                        if (on) n.delete(k);
                        else n.add(k);
                        return n;
                      })
                    }
                    className={cn("h-6 rounded-md border px-2 text-[11px]", on ? "border-accent/60 bg-accent/15" : "border-border text-fg-muted hover:bg-bg-hover")}
                  >
                    {KIND_LABEL[k]} <span className="tabular text-fg-subtle">{counts[k]}</span>
                  </button>
                );
              })}
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            {diff.data.changes.length === 0 ? (
              <EmptyState compact title="No changes" description="Entities and tracked properties are identical between these snapshots." />
            ) : (
              <table className="w-full text-xs">
                <thead className="sticky top-0 bg-bg-panel text-left text-[11px] text-fg-subtle">
                  <tr>
                    <th className="px-4 py-2 font-medium">Change</th>
                    <th className="px-4 py-2 font-medium">Entity</th>
                    <th className="px-4 py-2 font-medium">Field</th>
                    <th className="px-4 py-2 font-medium">Before</th>
                    <th className="px-4 py-2 font-medium">After</th>
                  </tr>
                </thead>
                <tbody>
                  {changes.slice(0, 2000).map((c, i) => (
                    <tr key={`${c.entityId}-${c.field ?? ""}-${i}`} className="border-t border-border align-top">
                      <td className="px-4 py-1.5">
                        <Badge tone={KIND_TONE[c.kind]}>{KIND_LABEL[c.kind]}</Badge>
                      </td>
                      <td className="px-4 py-1.5">
                        <button type="button" className="text-left hover:text-accent" onClick={() => navigate("topology", { entityId: c.entityId })} title={c.entityId}>
                          <span className="mr-1.5 text-[10.5px] uppercase text-fg-subtle">{c.entityType}</span>
                          {c.displayName ?? c.entityId}
                        </button>
                      </td>
                      <td className="px-4 py-1.5 font-mono text-fg-muted">{c.field ?? ""}</td>
                      <td className="max-w-[240px] break-all px-4 py-1.5 font-mono text-fg-muted">{c.kind === "changed" ? show(c.before) : ""}</td>
                      <td className="max-w-[240px] break-all px-4 py-1.5 font-mono">{c.kind === "changed" ? show(c.after) : ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {changes.length > 2000 && <p className="p-3 text-[11px] text-fg-subtle">Showing 2000 of {changes.length} changes.</p>}
            {diff.data.notes.length > 0 && (
              <ul className="space-y-0.5 p-4 text-[11px] text-fg-subtle">
                {diff.data.notes.map((n) => (
                  <li key={n}>· {n}</li>
                ))}
              </ul>
            )}
          </div>
        </>
      )}
    </div>
  );
}
