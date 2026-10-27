import { CheckCircle2, CircleSlash, Lock, XCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Banner, Card, EmptyState, SectionHeader, Spinner, Stat } from "@/components/ui/misc";
import { fmtBytes, fmtDateTime, fmtNumber, fmtPercent } from "@/lib/format";
import { useAppStore } from "@/stores/app-store";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";
import type { PaneProps } from "./InsightsWorkspace";
import { MetricChart } from "./MetricChart";

export function OverviewPane({ system, version, navigate }: PaneProps) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const ov = useAsync(() => insightsApi.overview(system.id), [system.id, version]);

  if (!ov.data) {
    if (ov.error) return <EmptyState title="Could not load overview" description={ov.error} action={<Button size="sm" onClick={ov.reload}>Retry</Button>} />;
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading overview…" />
      </div>
    );
  }
  const o = ov.data;
  if (!o.epochId) {
    return (
      <EmptyState
        title="No data collected yet"
        description={
          o.lastError
            ? `The last attempt failed: ${o.lastError}`
            : o.enabled
              ? "The first collection runs shortly. Use “Collect now” to start immediately."
              : "Automatic collection is paused. Use “Collect now” or enable it under Systems."
        }
      />
    );
  }

  const missing = "Not reported in the latest snapshot";
  const jsOff = o.jetstream === "disabled" ? "JetStream disabled" : o.jetstream === "unknown" ? "JetStream status unknown" : missing;

  return (
    <div className="h-full overflow-auto">
      <div className="space-y-5 p-5">
        {o.demo && (
          <Banner tone="warning" className="rounded-md">
            This system shows <strong>simulated demo data</strong>. Nothing here reflects a real server.
          </Banner>
        )}
        {o.notes.length > 0 && (
          <ul className="space-y-1 text-xs text-fg-muted">
            {o.notes.map((n) => (
              <li key={n}>· {n}</li>
            ))}
          </ul>
        )}

        <div className="grid grid-cols-2 gap-2 md:grid-cols-3 xl:grid-cols-6">
          <Stat label="Servers" value={o.servers !== null ? fmtNumber(o.servers) : null} unknownReason={missing} onClick={() => navigate("topology")} />
          <Stat label="Client connections" sub="monitored server" value={o.connections !== null ? fmtNumber(o.connections) : null} unknownReason={missing} />
          <Stat label="Subscriptions" value={o.subscriptions !== null ? fmtNumber(o.subscriptions, { compact: true }) : null} unknownReason={missing} />
          <Stat label="Streams" value={o.streams !== null ? fmtNumber(o.streams) : null} unknownReason={jsOff} />
          <Stat label="Consumers" value={o.consumers !== null ? fmtNumber(o.consumers) : null} unknownReason={jsOff} />
          <Stat
            label="Slow consumers"
            value={o.slowConsumers !== null ? fmtNumber(o.slowConsumers) : null}
            tone={o.slowConsumers ? "warning" : "default"}
            sub="cumulative since server start"
            unknownReason={missing}
          />
          <Stat label="CPU" value={o.cpu !== null ? fmtPercent(o.cpu, 1) : null} unknownReason={missing} sub="monitored server" />
          <Stat label="Memory" value={o.memBytes !== null ? fmtBytes(o.memBytes) : null} unknownReason={missing} />
          <Stat label="Messages in" value={o.inMsgs !== null ? fmtNumber(o.inMsgs, { compact: true }) : null} sub="total since start" unknownReason={missing} />
          <Stat label="Messages out" value={o.outMsgs !== null ? fmtNumber(o.outMsgs, { compact: true }) : null} sub="total since start" unknownReason={missing} />
          <Stat
            label="Open findings"
            value={fmtNumber(o.criticalFindings + o.warningFindings + o.infoFindings)}
            tone={o.criticalFindings > 0 ? "danger" : o.warningFindings > 0 ? "warning" : "success"}
            sub={`${o.criticalFindings} critical · ${o.warningFindings} warning · ${o.infoFindings} info`}
            onClick={() => navigate("findings")}
          />
          <Stat label="JetStream" value={o.jetstream} tone={o.jetstream === "enabled" ? "success" : "default"} sub={o.version ? `nats-server ${o.version}` : undefined} />
        </div>

        {o.serverEntityId ? (
          <div className="space-y-2">
            <SectionHeader
              title="Traffic"
              description="The monitored server. Rates are derived from its counters between snapshots; gaps mean a sample was missing or the counter reset."
            />
            <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-4">
              <MetricChart systemId={system.id} entityId={o.serverEntityId} metric="in_msgs" title="Messages in" version={version} />
              <MetricChart systemId={system.id} entityId={o.serverEntityId} metric="out_msgs" title="Messages out" version={version} color="var(--color-chart-2)" />
              <MetricChart systemId={system.id} entityId={o.serverEntityId} metric="connections" title="Connections" version={version} color="var(--color-chart-3)" />
              <MetricChart systemId={system.id} entityId={o.serverEntityId} metric="mem" title="Memory" unit="bytes" version={version} color="var(--color-chart-4)" />
            </div>
          </div>
        ) : null}

        <Card>
          <div className="border-b border-border px-4 py-2.5">
            <SectionHeader title="Endpoint coverage" description={`Snapshot ${fmtDateTime(o.collectedAt, tz)}`} />
          </div>
          <table className="w-full text-xs">
            <thead className="text-left text-[11px] text-fg-subtle">
              <tr>
                <th className="px-4 py-2 font-medium">Endpoint</th>
                <th className="px-4 py-2 font-medium">Result</th>
                <th className="px-4 py-2 font-medium">Detail</th>
                <th className="px-4 py-2 text-right font-medium">Time</th>
              </tr>
            </thead>
            <tbody>
              {o.endpointStatus.map((e) => (
                <tr key={e.endpoint} className="border-t border-border">
                  <td className="px-4 py-2 font-mono">/{e.endpoint.replace(/^\/+/, "")}</td>
                  <td className="px-4 py-2">
                    {e.ok ? (
                      <span className="inline-flex items-center gap-1 text-success">
                        <CheckCircle2 className="h-3.5 w-3.5" /> ok
                      </span>
                    ) : e.permissionDenied ? (
                      <span className="inline-flex items-center gap-1 text-warning">
                        <Lock className="h-3.5 w-3.5" /> forbidden
                      </span>
                    ) : !e.available ? (
                      <span className="inline-flex items-center gap-1 text-fg-subtle">
                        <CircleSlash className="h-3.5 w-3.5" /> not available
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-danger">
                        <XCircle className="h-3.5 w-3.5" /> failed
                      </span>
                    )}
                  </td>
                  <td className="max-w-[420px] truncate px-4 py-2 text-fg-muted" title={e.error}>
                    {e.httpCode ? <Badge className="mr-1.5">HTTP {e.httpCode}</Badge> : null}
                    {e.error ?? ""}
                  </td>
                  <td className="tabular px-4 py-2 text-right text-fg-subtle">{e.durationMs !== undefined ? `${e.durationMs} ms` : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </div>
    </div>
  );
}
