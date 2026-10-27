import { Copy, Pencil, Plug, PlugZap, Radio, RefreshCw, Send, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, connectionTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Banner, Card, EmptyState, KeyValue, PageHeader, SectionHeader } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { fmtDateTime } from "@/lib/format";
import { useAppStore } from "@/stores/app-store";
import { confirm, toast } from "@/stores/ui-store";
import type { BrokerCapabilities, ConnectionProfile, HealthStatus } from "@/types/models";

const CAP_LABELS: Record<string, string> = {
  connect: "Connect",
  publish: "Publish",
  subscribe: "Subscribe",
  request_reply: "Request / reply",
  explore: "Subject explorer",
  jetstream: "JetStream",
  queue_manage: "Queue management",
  metrics: "Metrics",
  history: "History",
};

const AUTH_TEXT: Record<string, string> = {
  none: "None",
  user_pass: "Username and password",
  token: "Token",
  nkey: "NKey",
  creds: "Credentials file",
  tls_cert: "TLS client certificate",
};

export function ConnectionDetails({ connectionId }: { connectionId: string }) {
  const conn = useAppStore((s) => s.connections.find((c) => c.id === connectionId));
  const status = useAppStore((s) => s.runtimeStatus[connectionId] ?? conn?.lastStatus ?? "disconnected");
  const detail = useAppStore((s) => s.statusDetail[connectionId]);
  const allSubs = useAppStore((s) => s.subscriptions);
  const subs = useMemo(() => allSubs.filter((x) => x.connectionId === connectionId), [allSubs, connectionId]);
  const tz = useAppStore((s) => s.preferences.timezone);
  const { connect, disconnect, openTab, closeTab, refreshConnections } = useAppStore.getState();
  const [busy, setBusy] = useState<string | null>(null);
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [healthError, setHealthError] = useState<string | null>(null);
  const [caps, setCaps] = useState<BrokerCapabilities | null>(null);

  const connected = status === "connected";

  const loadHealth = useCallback(async () => {
    if (!connected) {
      setHealth(null);
      return;
    }
    try {
      setHealth(await engineRequest<HealthStatus>("connections.health", { id: connectionId }));
      setHealthError(null);
    } catch (e) {
      setHealthError(errorMessage(e));
    }
  }, [connected, connectionId]);

  useEffect(() => {
    void loadHealth();
  }, [loadHealth]);

  useEffect(() => {
    engineRequest<BrokerCapabilities>("connections.capabilities", { id: connectionId })
      .then(setCaps)
      .catch(() => setCaps(null));
  }, [connectionId, conn?.readOnly]);

  if (!conn) {
    return <EmptyState title="Connection not found" description="It may have been deleted." />;
  }

  async function run(label: string, fn: () => Promise<unknown>) {
    setBusy(label);
    try {
      await fn();
    } finally {
      setBusy(null);
    }
  }

  async function remove() {
    if (!conn) return;
    const ok = await confirm({
      title: `Delete ${conn.name}?`,
      description: "The connection profile and its stored secret are removed. Active subscriptions on it are stopped.",
      confirmLabel: "Delete",
      tone: "danger",
      requireText: conn.environment === "production" ? conn.name : undefined,
    });
    if (!ok) return;
    await run("delete", async () => {
      try {
        await engineRequest("connections.delete", { id: conn.id });
        toast.success("Connection deleted", conn.name);
        closeTab(`conn-${conn.id}`);
        await refreshConnections();
      } catch (e) {
        toast.error("Could not delete connection", errorMessage(e));
      }
    });
  }

  async function duplicate() {
    if (!conn) return;
    await run("duplicate", async () => {
      try {
        const copy = await engineRequest<ConnectionProfile>("connections.duplicate", { id: conn.id });
        await refreshConnections();
        openTab({ id: `conn-${copy.id}`, kind: "connection", title: copy.name, connectionId: copy.id, mode: "edit" });
      } catch (e) {
        toast.error("Could not duplicate connection", errorMessage(e));
      }
    });
  }

  const envLabel = conn.environment === "custom" ? conn.environmentName || "custom" : conn.environment;
  const capList = caps?.capabilities ?? [];

  return (
    <div className="h-full overflow-auto">
        <PageHeader
          title={conn.name}
          description={<span className="font-mono text-xs">{conn.servers.join(", ")}</span>}
          meta={
            <>
              <Badge tone={connectionTone(status)} dot>
                {status}
              </Badge>
              <Badge tone={conn.environment === "production" ? "production" : "neutral"}>{envLabel}</Badge>
              {conn.readOnly && <Badge tone="info">read-only</Badge>}
            </>
          }
          actions={
            <>
              {connected || status === "reconnecting" || status === "connecting" ? (
                <Button variant="secondary" loading={busy === "disconnect"} disabled={busy !== null} onClick={() => void run("disconnect", () => disconnect(conn.id))}>
                  <Plug className="h-3.5 w-3.5" /> Disconnect
                </Button>
              ) : (
                <Button loading={busy === "connect"} disabled={busy !== null} onClick={() => void run("connect", () => connect(conn.id))}>
                  <PlugZap className="h-3.5 w-3.5" /> Connect
                </Button>
              )}
              <Button variant="outline" disabled={busy !== null} onClick={() => openTab({ id: `conn-${conn.id}`, kind: "connection", title: conn.name, connectionId: conn.id, mode: "edit" })}>
                <Pencil className="h-3.5 w-3.5" /> Edit
              </Button>
              <Button variant="ghost" size="icon" aria-label="Duplicate connection" title="Duplicate" disabled={busy !== null} onClick={() => void duplicate()}>
                <Copy className="h-4 w-4" />
              </Button>
              <Button variant="ghost" size="icon" aria-label="Delete connection" title="Delete" disabled={busy !== null} onClick={() => void remove()}>
                <Trash2 className="h-4 w-4 text-danger" />
              </Button>
            </>
          }
        />
      <div className="mx-auto max-w-3xl space-y-5 p-6">
        {status === "error" && detail && <Banner tone="danger" className="rounded-md">{detail}</Banner>}
        {status === "reconnecting" && <Banner tone="warning" className="rounded-md">Connection lost. Reconnecting{detail ? ` — ${detail}` : "…"}</Banner>}

        {connected && (
          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                openTab({ id: `msgs-${conn.id}`, kind: "messages", title: `${conn.name} · live`, connectionId: conn.id });
              }}
            >
              <Radio className="h-3.5 w-3.5" /> Open live messages
            </Button>
            <Button
              variant="secondary"
              size="sm"
              disabled={conn.readOnly}
              title={conn.readOnly ? "This connection is read-only" : undefined}
              onClick={() => {
                openTab({ id: `pub-${conn.id}`, kind: "publisher", title: `${conn.name} · publish`, connectionId: conn.id });
              }}
            >
              <Send className="h-3.5 w-3.5" /> Publish
            </Button>
          </div>
        )}

        <Card className="p-4">
          <SectionHeader
            title="Server"
            actions={
              connected ? (
                <Button size="xs" variant="ghost" onClick={() => void loadHealth()} aria-label="Refresh server info">
                  <RefreshCw className="h-3 w-3" /> Refresh
                </Button>
              ) : undefined
            }
          />
          {!connected ? (
            <p className="text-xs text-fg-subtle">Connect to see the server this session is attached to.</p>
          ) : healthError ? (
            <p className="text-xs text-danger">{healthError}</p>
          ) : (
            <KeyValue
              items={[
                ["Connected to", health?.details?.server],
                ["Server name", health?.details?.serverName],
                ["Server version", health?.details?.version],
                ["Server ID", health?.details?.serverId ? <span className="font-mono text-[11px]">{health.details.serverId}</span> : undefined],
                ["Round-trip time", health?.details?.rttMs ? `${health.details.rttMs} ms` : undefined],
                ["JetStream", health?.details?.jetstream],
              ]}
            />
          )}
        </Card>

        <Card className="p-4">
          <SectionHeader title="Configuration" />
          <KeyValue
            items={[
              ["Authentication", `${AUTH_TEXT[conn.auth.method] ?? conn.auth.method}${conn.auth.username ? ` (${conn.auth.username})` : ""}`],
              [
                "Stored secret",
                ["user_pass", "token", "nkey"].includes(conn.auth.method) ? (conn.auth.secretRef ? "Yes" : <span className="text-warning">Missing</span>) : "Not used",
              ],
              ["Credentials file", conn.auth.credsPath],
              ["TLS", conn.tls.enabled ? "Required, certificate verified" : "Off"],
              ["CA certificate", conn.tls.caCertPath],
              ["Client certificate", conn.tls.clientCertPath],
              ["Connect timeout", `${conn.timeoutMs} ms`],
              [
                "Reconnect",
                conn.reconnect.enabled
                  ? `${conn.reconnect.initialBackoffMs}–${conn.reconnect.maxBackoffMs} ms backoff, ${conn.reconnect.maxAttempts === 0 ? "unlimited" : conn.reconnect.maxAttempts} attempts`
                  : "Off",
              ],
              ["Last connected", conn.lastConnectedAt ? fmtDateTime(conn.lastConnectedAt, tz) : "Never"],
              ["Created", fmtDateTime(conn.createdAt, tz)],
            ].filter(([, v]) => v !== undefined && v !== "") as [React.ReactNode, React.ReactNode][]}
          />
        </Card>

        <Card className="p-4">
          <SectionHeader title="Capabilities" description="What Relay can do with this connection." />
          {caps ? (
            <div className="flex flex-wrap gap-1.5">
              {Object.keys(CAP_LABELS)
                .filter((k) => capList.includes(k) || ["publish", "request_reply"].includes(k))
                .map((k) => (
                  <Badge key={k} tone={capList.includes(k) ? "success" : "neutral"}>
                    {CAP_LABELS[k]}
                    {!capList.includes(k) && " — off"}
                  </Badge>
                ))}
            </div>
          ) : (
            <p className="text-xs text-fg-subtle">Unknown</p>
          )}
          {caps?.limitations && caps.limitations.length > 0 && (
            <ul className="mt-3 list-disc space-y-0.5 pl-4 text-[11px] text-fg-subtle">
              {caps.limitations.map((l) => (
                <li key={l}>{l}</li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="p-4">
          <SectionHeader title="Subscriptions" description="Active on this connection in this session." />
          {subs.length === 0 ? (
            <p className="text-xs text-fg-subtle">None.</p>
          ) : (
            <ul className="divide-y divide-border text-xs">
              {subs.map((s) => (
                <li key={s.id} className="flex items-center gap-3 py-1.5">
                  <span className="flex-1 truncate font-mono">{s.subject}</span>
                  <Badge tone={s.status === "active" ? "success" : s.status === "paused" ? "warning" : "danger"}>{s.status}</Badge>
                  <span className="tabular text-fg-subtle">{s.messageCount} msgs</span>
                  {s.droppedCount > 0 && <span className="tabular text-warning">{s.droppedCount} dropped</span>}
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}
