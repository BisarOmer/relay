import { Activity, Pause, Play, Square } from "lucide-react";
import { Badge, connectionTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, EmptyState, PageHeader, SectionHeader, Stat } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { fmtDateTime, fmtNumber } from "@/lib/format";
import { useAppStore } from "@/stores/app-store";
import { toast } from "@/stores/ui-store";

/** Client-side activity of this Relay session. Server-side monitoring lives in Insights. */
export function ActivityPane() {
  const connections = useAppStore((s) => s.connections);
  const runtimeStatus = useAppStore((s) => s.runtimeStatus);
  const statusDetail = useAppStore((s) => s.statusDetail);
  const subscriptions = useAppStore((s) => s.subscriptions);
  const buffered = useAppStore((s) => s.messages.length);
  const tz = useAppStore((s) => s.preferences.timezone);
  const { openTab, setActivity } = useAppStore.getState();

  const live = connections.filter((c) => {
    const st = runtimeStatus[c.id] ?? c.lastStatus;
    return st !== "disconnected";
  });
  const received = subscriptions.reduce((n, s) => n + s.messageCount, 0);
  const dropped = subscriptions.reduce((n, s) => n + s.droppedCount, 0);

  async function act(id: string, subject: string, action: "pause" | "resume" | "unsubscribe") {
    try {
      await engineRequest(`messages.${action}`, { id });
    } catch (e) {
      toast.error(`Could not ${action} ${subject}`, errorMessage(e));
    }
  }

  return (
    <div className="h-full overflow-auto">
      <PageHeader
        title="Session activity"
        description="Connections and subscriptions opened by this Relay session. For server-side health, use Insights."
        actions={
          <Button variant="outline" size="sm" onClick={() => setActivity("insights")}>
            Open Insights
          </Button>
        }
      />
      <div className="space-y-5 p-5">
        <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
          <Stat label="Active connections" value={fmtNumber(live.length)} sub={`of ${connections.length} saved`} />
          <Stat label="Subscriptions" value={fmtNumber(subscriptions.length)} />
          <Stat label="Messages received" value={fmtNumber(received)} sub={`${fmtNumber(buffered)} in view buffer`} />
          <Stat label="Not displayed" value={fmtNumber(dropped)} tone={dropped > 0 ? "warning" : "default"} sub="paused or buffer full" />
        </div>

        <Card>
          <div className="border-b border-border px-4 py-2.5">
            <SectionHeader title="Connections" />
          </div>
          {live.length === 0 ? (
            <EmptyState compact icon={Activity} title="No active connections" />
          ) : (
            <table className="w-full text-xs">
              <thead className="text-left text-[11px] text-fg-subtle">
                <tr>
                  <th className="px-4 py-2 font-medium">Name</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 font-medium">Detail</th>
                  <th className="px-4 py-2 font-medium">Last connected</th>
                </tr>
              </thead>
              <tbody>
                {live.map((c) => {
                  const st = runtimeStatus[c.id] ?? c.lastStatus;
                  return (
                    <tr key={c.id} className="border-t border-border">
                      <td className="px-4 py-2">
                        <button
                          type="button"
                          className="hover:text-accent"
                          onClick={() => {
                            setActivity("explorer");
                            openTab({ id: `conn-${c.id}`, kind: "connection", title: c.name, connectionId: c.id });
                          }}
                        >
                          {c.name}
                        </button>
                      </td>
                      <td className="px-4 py-2">
                        <Badge tone={connectionTone(st)} dot>
                          {st}
                        </Badge>
                      </td>
                      <td className="max-w-[320px] truncate px-4 py-2 text-fg-muted" title={statusDetail[c.id]}>
                        {statusDetail[c.id] || "—"}
                      </td>
                      <td className="px-4 py-2 text-fg-muted">{c.lastConnectedAt ? fmtDateTime(c.lastConnectedAt, tz) : "—"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Card>

        <Card>
          <div className="border-b border-border px-4 py-2.5">
            <SectionHeader title="Subscriptions" />
          </div>
          {subscriptions.length === 0 ? (
            <EmptyState compact title="No subscriptions" description="Start one from Live messages." />
          ) : (
            <table className="w-full text-xs">
              <thead className="text-left text-[11px] text-fg-subtle">
                <tr>
                  <th className="px-4 py-2 font-medium">Subject</th>
                  <th className="px-4 py-2 font-medium">Connection</th>
                  <th className="px-4 py-2 font-medium">Mode</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 text-right font-medium">Received</th>
                  <th className="px-4 py-2 text-right font-medium">Not displayed</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody>
                {subscriptions.map((s) => (
                  <tr key={s.id} className="border-t border-border">
                    <td className="px-4 py-2 font-mono">{s.subject}</td>
                    <td className="px-4 py-2">{connections.find((c) => c.id === s.connectionId)?.name ?? s.connectionId.slice(0, 8)}</td>
                    <td className="px-4 py-2">{s.mode}</td>
                    <td className="px-4 py-2">
                      <Badge tone={s.status === "active" ? "success" : s.status === "paused" ? "warning" : "danger"} title={s.error}>
                        {s.status}
                      </Badge>
                    </td>
                    <td className="tabular px-4 py-2 text-right">{fmtNumber(s.messageCount)}</td>
                    <td className={`tabular px-4 py-2 text-right ${s.droppedCount > 0 ? "text-warning" : ""}`}>{fmtNumber(s.droppedCount)}</td>
                    <td className="px-4 py-2 text-right">
                      <div className="inline-flex gap-0.5">
                        <Button
                          size="icon-xs"
                          variant="ghost"
                          aria-label={s.status === "paused" ? `Resume ${s.subject}` : `Pause ${s.subject}`}
                          onClick={() => void act(s.id, s.subject, s.status === "paused" ? "resume" : "pause")}
                        >
                          {s.status === "paused" ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
                        </Button>
                        <Button size="icon-xs" variant="ghost" aria-label={`Unsubscribe ${s.subject}`} onClick={() => void act(s.id, s.subject, "unsubscribe")}>
                          <Square className="h-3 w-3" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </div>
    </div>
  );
}
