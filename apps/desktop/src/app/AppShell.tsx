import { ChevronDown, Terminal, Trash2, X } from "lucide-react";
import { lazy, Suspense, useEffect } from "react";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { Banner } from "@/components/ui/misc";
import { Button } from "@/components/ui/button";
import { ConfirmHost, Toaster } from "@/components/ui/overlays";
import { CommandPalette } from "@/features/command-palette/CommandPalette";
import { ConnectionDetails } from "@/features/connections/ConnectionDetails";
import { ConnectionForm } from "@/features/connections/ConnectionForm";
import { ExplorerSidebar } from "@/features/explorer/ExplorerSidebar";
import { StreamView } from "@/features/explorer/StreamView";
import { MessageInspector } from "@/features/messages/MessageInspector";
import { ActivityPane } from "@/features/monitoring/ActivityPane";
import { Publisher } from "@/features/publisher/Publisher";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { fmtTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { Header } from "./Header";
import { ShortcutsDialog, useGlobalShortcuts } from "./Shortcuts";
import { Sidebar } from "./Sidebar";
import { ConnectionPicker, Welcome } from "./Welcome";

const InsightsWorkspace = lazy(() => import("@/features/insights/InsightsWorkspace").then((m) => ({ default: m.InsightsWorkspace })));

export function AppShell() {
  const bootstrap = useAppStore((s) => s.bootstrap);
  const activity = useAppStore((s) => s.activity);
  const engineState = useAppStore((s) => s.engineState);
  const engineError = useAppStore((s) => s.engineError);
  const restart = useAppStore((s) => s.restartEngine);

  useGlobalShortcuts();

  useEffect(() => {
    let unlisten: (() => void) | undefined;
    let cancelled = false;
    void bootstrap().then((u) => {
      if (cancelled) u();
      else unlisten = u;
    });
    return () => {
      cancelled = true;
      unlisten?.();
    };
  }, [bootstrap]);

  const tabbed = activity === "explorer" || activity === "messages" || activity === "publisher";

  return (
    <div className="flex h-full">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        <Header />
        {(engineState === "error" || engineState === "unavailable") && (
          <Banner
            tone="danger"
            className="border-x-0 border-t-0"
            action={
              engineState === "error" ? (
                <Button size="xs" variant="danger" onClick={() => void restart()}>
                  Restart engine
                </Button>
              ) : undefined
            }
          >
            <span className="font-medium">The Relay engine is not running.</span> {engineError}
          </Banner>
        )}
        <div className="flex min-h-0 flex-1">
          {tabbed && (
            <aside className="w-64 shrink-0 border-r border-border bg-bg-panel" aria-label="Connections">
              <ExplorerSidebar />
            </aside>
          )}
          <main className="flex min-w-0 flex-1 flex-col">
            <ErrorBoundary resetKey={activity} label="page">
              {activity === "insights" && (
                <Suspense fallback={<div className="flex-1" aria-busy="true" />}>
                  <InsightsWorkspace />
                </Suspense>
              )}
              {activity === "settings" && <SettingsPage />}
              {activity === "monitoring" && <ActivityPane />}
              {tabbed && <TabbedWorkspace />}
            </ErrorBoundary>
            <OutputPanel />
          </main>
        </div>
        <StatusBar />
      </div>
      <CommandPalette />
      <ShortcutsDialog />
      <ConfirmHost />
      <Toaster />
    </div>
  );
}

function TabbedWorkspace() {
  const tabs = useAppStore((s) => s.tabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const setActiveTab = useAppStore((s) => s.setActiveTab);
  const closeTab = useAppStore((s) => s.closeTab);
  const activeTab = tabs.find((t) => t.id === activeTabId) ?? tabs[0];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {!(tabs.length === 1 && tabs[0].kind === "welcome") && (
        <div role="tablist" aria-label="Open editors" className="flex h-9 shrink-0 items-end gap-px overflow-x-auto border-b border-border bg-bg-panel px-1">
          {tabs.map((tab) => {
            const active = activeTab?.id === tab.id;
            return (
              <div
                key={tab.id}
                className={cn(
                  "group flex h-8 max-w-[220px] items-center gap-1 rounded-t-md border border-b-0 pl-3 pr-1 text-xs",
                  active ? "border-border bg-bg text-fg" : "border-transparent text-fg-muted hover:bg-bg-hover hover:text-fg",
                )}
                onMouseDown={(e) => {
                  if (e.button === 1) {
                    e.preventDefault();
                    closeTab(tab.id);
                  }
                }}
              >
                <button type="button" role="tab" aria-selected={active} className="truncate" onClick={() => setActiveTab(tab.id)}>
                  {tab.title}
                </button>
                <button
                  type="button"
                  aria-label={`Close ${tab.title}`}
                  className={cn("rounded p-0.5 text-fg-subtle hover:bg-bg-active hover:text-fg", !active && "opacity-0 group-hover:opacity-100")}
                  onClick={() => closeTab(tab.id)}
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            );
          })}
        </div>
      )}
      <div className="min-h-0 flex-1 bg-bg">
        <ErrorBoundary resetKey={activeTab?.id} label="tab">
        {activeTab?.kind === "welcome" && <Welcome />}
        {activeTab?.kind === "connection" &&
          (activeTab.mode === "new" || !activeTab.connectionId ? (
            <ConnectionForm tabId={activeTab.id} />
          ) : activeTab.mode === "edit" ? (
            <ConnectionForm tabId={activeTab.id} connectionId={activeTab.connectionId} />
          ) : (
            <ConnectionDetails connectionId={activeTab.connectionId} />
          ))}
        {activeTab?.kind === "messages" &&
          (activeTab.connectionId ? (
            <MessageInspector key={activeTab.id} connectionId={activeTab.connectionId} initialSubject={activeTab.subject} />
          ) : (
            <ConnectionPicker tabId={activeTab.id} purpose="messages" />
          ))}
        {activeTab?.kind === "publisher" && <Publisher connectionId={activeTab.connectionId} />}
        {activeTab?.kind === "stream" && (
          <StreamView key={activeTab.id} connectionId={activeTab.connectionId} stream={activeTab.stream} />
        )}
        {activeTab?.kind === "monitoring" && <ActivityPane />}
        </ErrorBoundary>
      </div>
    </div>
  );
}

function OutputPanel() {
  const open = useAppStore((s) => s.bottomOpen);
  const setOpen = useAppStore((s) => s.setBottomOpen);
  const log = useAppStore((s) => s.bottomLog);
  const clear = useAppStore((s) => s.clearLog);
  const tz = useAppStore((s) => s.preferences.timezone);
  if (!open) return null;
  return (
    <section aria-label="Output" className="flex h-44 shrink-0 flex-col border-t border-border bg-bg-panel">
      <div className="flex h-8 items-center gap-2 border-b border-border px-3 text-xs text-fg-subtle">
        <Terminal className="h-3.5 w-3.5" /> Output
        <span className="tabular">{log.length}</span>
        <div className="ml-auto flex gap-1">
          <Button size="icon-xs" variant="ghost" aria-label="Clear output" onClick={clear}>
            <Trash2 className="h-3 w-3" />
          </Button>
          <Button size="icon-xs" variant="ghost" aria-label="Hide output" onClick={() => setOpen(false)}>
            <ChevronDown className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto px-3 py-1.5 font-mono text-[11px] leading-5">
        {log.length === 0 && <div className="text-fg-subtle">No output yet.</div>}
        {log.map((l, i) => (
          <div key={i} className={cn("flex gap-3", l.level === "error" ? "text-danger" : l.level === "warn" ? "text-warning" : "text-fg-muted")}>
            <span className="shrink-0 text-fg-subtle">{fmtTime(l.at, tz)}</span>
            <span className="break-all">{l.text}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function StatusBar() {
  const connections = useAppStore((s) => s.connections);
  const runtimeStatus = useAppStore((s) => s.runtimeStatus);
  const subscriptions = useAppStore((s) => s.subscriptions);
  const messages = useAppStore((s) => s.messages.length);
  const max = useAppStore((s) => s.preferences.maxDisplayedMessages);
  const bottomOpen = useAppStore((s) => s.bottomOpen);
  const setBottomOpen = useAppStore((s) => s.setBottomOpen);
  const errors = useAppStore((s) => s.bottomLog.filter((l) => l.level === "error").length);
  const connected = connections.filter((c) => runtimeStatus[c.id] === "connected").length;
  const reconnecting = connections.filter((c) => runtimeStatus[c.id] === "reconnecting").length;
  const setActivity = useAppStore((s) => s.setActivity);

  return (
    <footer className="flex h-6 shrink-0 items-center gap-4 border-t border-border bg-bg-panel px-3 text-[11px] text-fg-subtle">
      <button type="button" className="hover:text-fg" onClick={() => setActivity("explorer")}>
        <span className="tabular">{connected}</span>/{connections.length} connected
        {reconnecting > 0 && <span className="ml-1 text-warning">· {reconnecting} reconnecting</span>}
      </button>
      <button type="button" className="hover:text-fg" onClick={() => setActivity("monitoring")}>
        <span className="tabular">{subscriptions.length}</span> subscriptions
      </button>
      <span title="Messages kept in the live inspector buffer">
        <span className="tabular">{messages}</span>/{max} buffered
      </span>
      <button type="button" className="ml-auto flex items-center gap-1 hover:text-fg" onClick={() => setBottomOpen(!bottomOpen)}>
        <Terminal className="h-3 w-3" /> Output
        {errors > 0 && <span className="rounded bg-danger/15 px-1 text-danger">{errors}</span>}
      </button>
    </footer>
  );
}
