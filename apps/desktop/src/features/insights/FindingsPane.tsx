import { BellOff, Bell, CheckCircle2, ShieldCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge, severityTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Segmented, Select } from "@/components/ui/input";
import { Card, EmptyState, KeyValue, SectionHeader, Spinner } from "@/components/ui/misc";
import { errorMessage } from "@/lib/engine";
import { fmtDateTime, fmtRelative } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { confirm, toast } from "@/stores/ui-store";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";
import type { PaneProps } from "./InsightsWorkspace";
import type { Finding } from "./types";

type StatusFilter = "open" | "all" | "resolved";
const SEV_RANK = { critical: 0, warning: 1, info: 2 } as const;

const EVENT_LABEL: Record<string, string> = {
  opened: "Opened",
  severity_changed: "Severity changed",
  resolved: "Resolved (condition cleared)",
  reopened: "Reopened",
  acknowledged: "Acknowledged",
  unacknowledged: "Acknowledgement removed",
  resolved_manually: "Resolved manually",
};

export function FindingsPane({ system, version, focus, navigate }: PaneProps) {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("open");
  const [severity, setSeverity] = useState<"all" | Finding["severity"]>("all");
  const [hideAcked, setHideAcked] = useState(false);
  const [selected, setSelected] = useState<string | null>(focus.findingId ?? null);
  const [mutation, setMutation] = useState(0);
  const list = useAsync(() => insightsApi.findings(system.id, statusFilter !== "open"), [system.id, statusFilter, version, mutation]);

  useEffect(() => {
    if (focus.findingId) {
      setSelected(focus.findingId);
      setStatusFilter("all");
    }
  }, [focus.findingId]);

  const findings = useMemo(() => {
    let l = list.data ?? [];
    if (statusFilter === "resolved") l = l.filter((f) => f.status === "resolved");
    if (severity !== "all") l = l.filter((f) => f.severity === severity);
    if (hideAcked) l = l.filter((f) => !f.acknowledged);
    return [...l].sort(
      (a, b) =>
        Number(a.status === "resolved") - Number(b.status === "resolved") ||
        SEV_RANK[a.severity] - SEV_RANK[b.severity] ||
        b.lastObservedAt.localeCompare(a.lastObservedAt),
    );
  }, [list.data, statusFilter, severity, hideAcked]);

  const current = findings.find((f) => f.id === selected) ?? (list.data ?? []).find((f) => f.id === selected) ?? null;

  return (
    <div className="grid h-full min-h-0 grid-cols-[minmax(240px,34%)_minmax(0,1fr)]">
      <div className="flex min-h-0 flex-col border-r border-border">
        <div className="flex flex-wrap items-center gap-2 border-b border-border p-2">
          <Segmented<StatusFilter>
            size="xs"
            ariaLabel="Status"
            value={statusFilter}
            onChange={setStatusFilter}
            options={[
              { value: "open", label: "Open" },
              { value: "resolved", label: "Resolved" },
              { value: "all", label: "All" },
            ]}
          />
          <Select aria-label="Severity" className="h-6 w-auto py-0 text-[11px]" value={severity} onChange={(e) => setSeverity(e.target.value as typeof severity)}>
            <option value="all">All severities</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="info">Info</option>
          </Select>
          <label className="flex items-center gap-1.5 text-[11px] text-fg-muted">
            <input type="checkbox" checked={hideAcked} onChange={(e) => setHideAcked(e.target.checked)} className="h-3 w-3 accent-[var(--color-accent)]" />
            Hide acknowledged
          </label>
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {!list.data && list.loading && (
            <div className="p-4">
              <Spinner label="Loading findings…" />
            </div>
          )}
          {list.error && <p className="p-3 text-xs text-danger">{list.error}</p>}
          {list.data && findings.length === 0 && (
            <EmptyState
              compact
              icon={ShieldCheck}
              title={statusFilter === "open" ? "No open findings" : "No findings"}
              description={statusFilter === "open" ? "Every check that could be evaluated passed on the latest snapshot." : undefined}
            />
          )}
          <ul>
            {findings.map((f) => (
              <li key={f.id}>
                <button
                  type="button"
                  onClick={() => setSelected(f.id)}
                  aria-current={selected === f.id}
                  className={cn(
                    "flex w-full flex-col gap-1 border-b border-border px-3 py-2.5 text-left",
                    selected === f.id ? "bg-bg-active" : "hover:bg-bg-hover",
                    f.status === "resolved" && "opacity-70",
                  )}
                >
                  <div className="flex items-center gap-2">
                    <Badge tone={severityTone(f.severity)}>{f.severity}</Badge>
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{f.title}</span>
                  </div>
                  <div className="flex items-center gap-2 text-[11px] text-fg-subtle">
                    <span>{f.status}</span>
                    {f.acknowledged && <span>· acknowledged</span>}
                    <span>· {f.occurrenceCount}×</span>
                    <span className="ml-auto">{fmtRelative(f.lastObservedAt)}</span>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <div className="min-h-0 overflow-auto">
        {current ? (
          <FindingDetail
            key={current.id}
            finding={current}
            onChanged={() => setMutation((m) => m + 1)}
            onEntity={(id) => navigate("topology", { entityId: id })}
            version={version}
          />
        ) : (
          <EmptyState title="Select a finding" description="Findings come from automated checks on each snapshot. Checks that lack data are skipped, not passed." />
        )}
      </div>
    </div>
  );
}

function FindingDetail({ finding: f, onChanged, onEntity, version }: { finding: Finding; onChanged: () => void; onEntity: (id: string) => void; version: number }) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const events = useAsync(() => insightsApi.findingEvents(f.id), [f.id, version, f.status, f.acknowledged]);
  const [busy, setBusy] = useState<string | null>(null);

  async function run(label: string, fn: () => Promise<unknown>, success: string) {
    setBusy(label);
    try {
      await fn();
      toast.success(success);
      onChanged();
    } catch (e) {
      toast.error("Action failed", errorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  const evidence = Object.entries(f.evidence ?? {});

  return (
    <div className="space-y-4 p-5">
      <div className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={severityTone(f.severity)}>{f.severity}</Badge>
            <Badge tone={f.status === "resolved" ? "success" : f.status === "reopened" ? "danger" : "neutral"}>{f.status}</Badge>
            <Badge>{f.category}</Badge>
            {f.acknowledged && <Badge tone="info">acknowledged</Badge>}
          </div>
          <h2 className="text-[15px] font-semibold">{f.title}</h2>
          <p className="text-[13px] leading-relaxed text-fg-muted">{f.summary}</p>
        </div>
        <div className="flex shrink-0 gap-2">
          {f.status !== "resolved" && (
            <Button
              size="sm"
              variant="outline"
              loading={busy === "ack"}
              disabled={busy !== null}
              onClick={() => void run("ack", () => insightsApi.ack(f.id, !f.acknowledged), f.acknowledged ? "Acknowledgement removed" : "Finding acknowledged")}
            >
              {f.acknowledged ? <Bell className="h-3.5 w-3.5" /> : <BellOff className="h-3.5 w-3.5" />}
              {f.acknowledged ? "Unacknowledge" : "Acknowledge"}
            </Button>
          )}
          {f.status !== "resolved" && (
            <Button
              size="sm"
              variant="secondary"
              loading={busy === "resolve"}
              disabled={busy !== null}
              onClick={async () => {
                const ok = await confirm({
                  title: "Mark as resolved?",
                  description: "If the check fails again on a later snapshot, the finding reopens with its history intact.",
                  confirmLabel: "Resolve",
                });
                if (ok) void run("resolve", () => insightsApi.resolve(f.id), "Finding resolved");
              }}
            >
              <CheckCircle2 className="h-3.5 w-3.5" /> Resolve
            </Button>
          )}
        </div>
      </div>

      <Card className="p-3">
        <KeyValue
          items={[
            [
              "Entity",
              f.entityId ? (
                <button type="button" className="font-mono text-accent hover:underline" onClick={() => onEntity(f.entityId!)}>
                  {f.entityId}
                </button>
              ) : undefined,
            ],
            ["Check", <span className="font-mono">{f.checkId}</span>],
            ["First observed", fmtDateTime(f.firstObservedAt, tz)],
            ["Last observed", fmtDateTime(f.lastObservedAt, tz)],
            ["Occurrences", f.occurrenceCount],
            ["Acknowledged", f.acknowledgedAt ? fmtDateTime(f.acknowledgedAt, tz) : undefined],
            ["Resolved", f.resolvedAt ? `${fmtDateTime(f.resolvedAt, tz)}${f.resolution ? ` (${f.resolution.replace(/_/g, " ")})` : ""}` : undefined],
          ]}
        />
      </Card>

      {evidence.length > 0 && (
        <Card className="p-3">
          <SectionHeader title="Evidence" description="Values from the snapshot that triggered this finding." className="mb-2" />
          <KeyValue
            items={evidence.map(([k, v]) => [
              <span className="font-mono">{k}</span>,
              <span className="font-mono">{typeof v === "object" ? JSON.stringify(v) : String(v)}</span>,
            ])}
          />
        </Card>
      )}

      {(f.remediation || (f.steps && f.steps.length > 0)) && (
        <Card className="p-3">
          <SectionHeader title="What to do" className="mb-2" />
          {f.remediation && <p className="mb-2 text-xs leading-relaxed text-fg-muted">{f.remediation}</p>}
          {f.steps && (
            <ol className="list-decimal space-y-1 pl-4 text-xs leading-relaxed">
              {f.steps.map((s, i) => (
                <li key={i}>{s}</li>
              ))}
            </ol>
          )}
        </Card>
      )}

      <Card className="p-3">
        <SectionHeader title="Timeline" className="mb-2" />
        {events.loading && !events.data && <Spinner />}
        {events.error && <p className="text-xs text-danger">{events.error}</p>}
        {events.data && events.data.length === 0 && <p className="text-xs text-fg-subtle">No events recorded.</p>}
        <ol className="relative space-y-2 border-l border-border pl-4">
          {(events.data ?? []).map((ev) => (
            <li key={ev.id} className="text-xs">
              <span className="absolute -left-[4.5px] mt-1 h-2 w-2 rounded-full border border-bg bg-fg-subtle" aria-hidden />
              <div className="flex items-center gap-2">
                <span className="font-medium">{EVENT_LABEL[ev.eventType] ?? ev.eventType}</span>
                <span className="text-fg-subtle">{fmtDateTime(ev.observedAt, tz)}</span>
              </div>
              {ev.eventType === "severity_changed" && ev.evidence && (
                <div className="text-[11px] text-fg-subtle">
                  {String(ev.evidence.from)} → {String(ev.evidence.to)}
                </div>
              )}
            </li>
          ))}
        </ol>
      </Card>
    </div>
  );
}
