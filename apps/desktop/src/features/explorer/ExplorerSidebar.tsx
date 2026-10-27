import { ChevronRight, Database, Plug, PlugZap, Plus, Radio, RefreshCw, Search, Send } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, StatusDot, connectionTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Spinner } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import type { ConnectionProfile, ResourceDescriptor } from "@/types/models";

export function ExplorerSidebar() {
  const connections = useAppStore((s) => s.connections);
  const loaded = useAppStore((s) => s.connectionsLoaded);
  const engineReady = useAppStore((s) => s.engineState === "ready");
  const refreshConnections = useAppStore((s) => s.refreshConnections);
  const openTab = useAppStore((s) => s.openTab);
  const [filter, setFilter] = useState("");
  const [refreshing, setRefreshing] = useState(false);

  const q = filter.trim().toLowerCase();
  const visible = q
    ? connections.filter((c) => c.name.toLowerCase().includes(q) || c.servers.some((s) => s.toLowerCase().includes(q)))
    : connections;

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-9 items-center justify-between border-b border-border pl-3 pr-1.5">
        <span className="text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-subtle">Connections</span>
        <div className="flex gap-0.5">
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Reload connections"
            title="Reload"
            disabled={!engineReady}
            onClick={async () => {
              setRefreshing(true);
              try {
                await refreshConnections();
              } finally {
                setRefreshing(false);
              }
            }}
          >
            <RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="New connection"
            title="New connection"
            disabled={!engineReady}
            onClick={() => openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" })}
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
      {connections.length > 4 && (
        <div className="border-b border-border p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-fg-subtle" />
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter connections"
              aria-label="Filter connections"
              className="h-7 w-full rounded-md border border-border bg-bg-inset pl-7 pr-2 text-xs placeholder:text-fg-subtle focus:border-accent focus:outline-none"
            />
          </div>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {!loaded && engineReady && (
          <div className="p-3">
            <Spinner label="Loading…" />
          </div>
        )}
        {loaded && connections.length === 0 && (
          <EmptyState
            compact
            title="No connections"
            description="Add a NATS server to browse subjects and streams."
            action={
              <Button size="sm" onClick={() => openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" })}>
                <Plus className="h-3.5 w-3.5" /> Add connection
              </Button>
            }
          />
        )}
        {loaded && connections.length > 0 && visible.length === 0 && (
          <p className="px-2 py-3 text-xs text-fg-subtle">No connections match “{filter}”.</p>
        )}
        <ul role="tree" aria-label="Connections" className="space-y-0.5">
          {visible.map((c) => (
            <ConnectionNode key={c.id} conn={c} />
          ))}
        </ul>
      </div>
    </div>
  );
}

function ConnectionNode({ conn }: { conn: ConnectionProfile }) {
  const status = useAppStore((s) => s.runtimeStatus[conn.id] ?? conn.lastStatus);
  const selected = useAppStore((s) => s.selectedConnectionId === conn.id);
  const { selectConnection, openTab, connect, disconnect } = useAppStore.getState();
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const connected = status === "connected";
  const active = connected || status === "connecting" || status === "reconnecting";

  return (
    <li role="treeitem" aria-expanded={connected ? expanded : undefined} aria-selected={selected}>
      <div className={cn("group flex h-8 items-center gap-1 rounded-md pr-1", selected ? "bg-bg-active" : "hover:bg-bg-hover")}>
        <button
          type="button"
          aria-label={expanded ? "Collapse" : "Expand"}
          disabled={!connected}
          onClick={() => setExpanded(!expanded)}
          className="flex h-6 w-5 shrink-0 items-center justify-center text-fg-subtle disabled:opacity-0"
        >
          <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", expanded && connected && "rotate-90")} />
        </button>
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          onClick={() => {
            selectConnection(conn.id);
            openTab({ id: `conn-${conn.id}`, kind: "connection", title: conn.name, connectionId: conn.id });
          }}
          onDoubleClick={() => !active && void connect(conn.id)}
          title={`${conn.name} — ${status}\n${conn.servers.join("\n")}`}
        >
          <StatusDot tone={connectionTone(status)} pulse={status === "connecting" || status === "reconnecting"} label={status} />
          <span className="truncate text-[13px]">{conn.name}</span>
          {conn.environment === "production" && (
            <Badge tone="production" className="shrink-0">
              prod
            </Badge>
          )}
        </button>
        <div className="flex shrink-0 gap-0.5 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100">
          {connected && (
            <>
              <Button
                size="icon-xs"
                variant="ghost"
                aria-label={`Live messages on ${conn.name}`}
                title="Live messages"
                onClick={() => {
                  openTab({ id: `msgs-${conn.id}`, kind: "messages", title: `${conn.name} · live`, connectionId: conn.id });
                }}
              >
                <Radio className="h-3 w-3" />
              </Button>
              {!conn.readOnly && (
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label={`Publish on ${conn.name}`}
                  title="Publish"
                  onClick={() => {
                    openTab({ id: `pub-${conn.id}`, kind: "publisher", title: `${conn.name} · publish`, connectionId: conn.id });
                  }}
                >
                  <Send className="h-3 w-3" />
                </Button>
              )}
            </>
          )}
          <Button
            size="icon-xs"
            variant="ghost"
            disabled={busy}
            aria-label={active ? `Disconnect ${conn.name}` : `Connect ${conn.name}`}
            title={active ? "Disconnect" : "Connect"}
            onClick={async () => {
              setBusy(true);
              try {
                if (active) await disconnect(conn.id);
                else if (await connect(conn.id)) setExpanded(true);
              } finally {
                setBusy(false);
              }
            }}
          >
            {active ? <Plug className="h-3 w-3" /> : <PlugZap className="h-3 w-3" />}
          </Button>
        </div>
      </div>
      {connected && expanded && <StreamList connectionId={conn.id} />}
    </li>
  );
}

function StreamList({ connectionId }: { connectionId: string }) {
  const openTab = useAppStore((s) => s.openTab);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const [state, setState] = useState<{ loading: boolean; error?: string; streams: ResourceDescriptor[] }>({ loading: true, streams: [] });

  useEffect(() => {
    let cancelled = false;
    engineRequest<{ resources: ResourceDescriptor[] }>("explorer.list", { connectionId })
      .then((r) => !cancelled && setState({ loading: false, streams: (r.resources ?? []).filter((x) => x.kind === "stream") }))
      .catch((e) => !cancelled && setState({ loading: false, error: errorMessage(e), streams: [] }));
    return () => {
      cancelled = true;
    };
  }, [connectionId]);

  return (
    <ul role="group" className="mb-1 ml-5 border-l border-border pl-1.5">
      <li className="px-2 pb-0.5 pt-1 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-fg-subtle">JetStream streams</li>
      {state.loading && (
        <li className="px-2 py-1">
          <Spinner label="Loading streams…" />
        </li>
      )}
      {state.error && <li className="px-2 py-1 text-[11px] text-fg-subtle">{state.error}</li>}
      {!state.loading && !state.error && state.streams.length === 0 && (
        <li className="px-2 py-1 text-[11px] text-fg-subtle">No streams visible to this account.</li>
      )}
      {state.streams.map((r) => {
        const id = `stream-${connectionId}-${r.name}`;
        return (
          <li key={r.id} role="treeitem" aria-selected={activeTabId === id}>
            <button
              type="button"
              className={cn(
                "flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-xs",
                activeTabId === id ? "bg-bg-active text-fg" : "text-fg-muted hover:bg-bg-hover hover:text-fg",
              )}
              onClick={() => openTab({ id, kind: "stream", title: r.name, connectionId, stream: r.name })}
            >
              <Database className="h-3 w-3 shrink-0 text-fg-subtle" />
              <span className="truncate">{r.name}</span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
