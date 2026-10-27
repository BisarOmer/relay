import { Cable, KeyRound, Plus, Radar, Radio, ShieldCheck } from "lucide-react";
import { Badge, StatusDot, connectionTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/misc";
import { Kbd } from "@/components/ui/misc";
import { useAppStore } from "@/stores/app-store";
import { MOD } from "./navigation";

export function Welcome() {
  const openTab = useAppStore((s) => s.openTab);
  const setActivity = useAppStore((s) => s.setActivity);
  const connections = useAppStore((s) => s.connections);
  const engineReady = useAppStore((s) => s.engineState === "ready");

  return (
    <div className="flex h-full items-center justify-center overflow-auto p-8">
      <div className="w-full max-w-xl space-y-8">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">Welcome to Relay</h1>
          <p className="text-[13px] leading-relaxed text-fg-muted">
            Connect to NATS servers to inspect live traffic, publish messages, and browse JetStream. Point Insights at a
            server's monitoring port to watch health, topology and diagnostics over time.
          </p>
        </div>
        <div className="grid gap-2 lg:grid-cols-2">
          <button
            type="button"
            disabled={!engineReady}
            onClick={() => openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" })}
            className="group flex items-start gap-3 rounded-lg border border-border bg-bg-panel p-4 text-left transition-colors hover:border-accent/50 hover:bg-bg-hover disabled:opacity-50"
          >
            <Plus className="mt-0.5 h-4 w-4 shrink-0 text-accent" />
            <span>
              <span className="block text-[13px] font-medium">Add a NATS connection</span>
              <span className="block text-xs text-fg-muted">Client URL, usually nats://host:4222</span>
            </span>
          </button>
          <button
            type="button"
            disabled={!engineReady}
            onClick={() => setActivity("insights")}
            className="group flex items-start gap-3 rounded-lg border border-border bg-bg-panel p-4 text-left transition-colors hover:border-accent/50 hover:bg-bg-hover disabled:opacity-50"
          >
            <Radar className="mt-0.5 h-4 w-4 shrink-0 text-accent" />
            <span>
              <span className="block text-[13px] font-medium">Monitor a server</span>
              <span className="block text-xs text-fg-muted">Monitoring URL, usually http://host:8222</span>
            </span>
          </button>
        </div>
        {connections.length > 0 && (
          <div className="space-y-2">
            <h2 className="text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-subtle">Recent connections</h2>
            <ul className="divide-y divide-border rounded-lg border border-border bg-bg-panel">
              {connections.slice(0, 5).map((c) => (
                <li key={c.id}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-3 px-3 py-2 text-left text-[13px] hover:bg-bg-hover"
                    onClick={() => openTab({ id: `conn-${c.id}`, kind: "connection", title: c.name, connectionId: c.id })}
                  >
                    <Cable className="h-3.5 w-3.5 text-fg-subtle" />
                    <span className="flex-1 truncate">{c.name}</span>
                    <span className="truncate font-mono text-[11px] text-fg-subtle">{c.servers[0]}</span>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
        <ul className="grid gap-2 text-xs text-fg-muted">
          <li className="flex items-center gap-2">
            <KeyRound className="h-3.5 w-3.5 shrink-0 text-fg-subtle" /> Passwords, tokens and NKey seeds are kept in the engine's secret store (the OS keychain when available), never in the database.
          </li>
          <li className="flex items-center gap-2">
            <ShieldCheck className="h-3.5 w-3.5 shrink-0 text-fg-subtle" /> Insights is read-only: it never publishes or changes server state.
          </li>
          <li className="flex flex-wrap items-center gap-1.5">
            <Kbd>{MOD}K</Kbd> <span>opens the command palette,</span> <Kbd>?</Kbd> <span>lists shortcuts</span>
          </li>
        </ul>
      </div>
    </div>
  );
}

/** Shown when a page needs a connection but none was chosen yet. */
export function ConnectionPicker({ tabId, purpose }: { tabId: string; purpose: "messages" }) {
  const connections = useAppStore((s) => s.connections);
  const runtimeStatus = useAppStore((s) => s.runtimeStatus);
  const { openTab, closeTab, connect } = useAppStore.getState();

  const open = (id: string, name: string) => {
    closeTab(tabId);
    openTab({ id: `msgs-${id}`, kind: purpose, title: `${name} · live`, connectionId: id });
  };

  if (connections.length === 0) {
    return (
      <EmptyState
        icon={Radio}
        title="No connections yet"
        description="Add a NATS connection to watch live traffic."
        action={
          <Button size="sm" onClick={() => openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" })}>
            <Plus className="h-3.5 w-3.5" /> Add connection
          </Button>
        }
      />
    );
  }
  return (
    <div className="mx-auto max-w-lg space-y-3 p-8">
      <h2 className="text-[15px] font-semibold">Choose a connection</h2>
      <p className="text-xs text-fg-muted">Live messages subscribe through one connection at a time. Each connection gets its own tab.</p>
      <ul className="divide-y divide-border rounded-lg border border-border bg-bg-panel">
        {connections.map((c) => {
          const st = runtimeStatus[c.id] ?? c.lastStatus;
          return (
            <li key={c.id} className="flex items-center gap-3 px-3 py-2 text-[13px]">
              <StatusDot tone={connectionTone(st)} label={st} />
              <span className="flex-1 truncate">{c.name}</span>
              {c.environment === "production" && <Badge tone="production">prod</Badge>}
              {st === "connected" ? (
                <Button size="xs" onClick={() => open(c.id, c.name)}>
                  Open
                </Button>
              ) : (
                <Button
                  size="xs"
                  variant="secondary"
                  onClick={async () => {
                    if (await connect(c.id)) open(c.id, c.name);
                  }}
                >
                  Connect and open
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
