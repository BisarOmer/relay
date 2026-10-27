import { ChevronRight, Moon, RotateCcw, Search, Sun } from "lucide-react";
import { useState } from "react";
import { Badge, StatusDot } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Kbd, KeyValue } from "@/components/ui/misc";
import { useAppStore } from "@/stores/app-store";
import { MOD, NAV_ITEMS } from "./navigation";

export function Header() {
  const activity = useAppStore((s) => s.activity);
  const tabs = useAppStore((s) => s.tabs);
  const activeTabId = useAppStore((s) => s.activeTabId);
  const crumbs = useAppStore((s) => s.crumbs);
  const setPalette = useAppStore((s) => s.setCommandPaletteOpen);
  const prefs = useAppStore((s) => s.preferences);
  const updatePrefs = useAppStore((s) => s.updatePreferences);

  const nav = NAV_ITEMS.find((n) => n.id === activity);
  const tab = tabs.find((t) => t.id === activeTabId);
  const tabbed = activity === "explorer" || activity === "messages" || activity === "publisher";
  const trail = [nav?.label ?? "", ...(tabbed && tab && tab.kind !== "welcome" ? [tab.title] : []), ...crumbs].filter(Boolean);
  const isLight = document.documentElement.dataset.theme === "light";

  return (
    <header className="flex h-11 shrink-0 items-center gap-3 overflow-hidden border-b border-border bg-bg-panel px-4">
      <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1 text-[13px]">
        <span className="text-fg-subtle">Relay</span>
        {trail.map((c, i) => (
          <span key={`${i}-${c}`} className="flex min-w-0 items-center gap-1">
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-fg-subtle" aria-hidden />
            <span className={i === trail.length - 1 ? "truncate font-medium text-fg" : "truncate text-fg-muted"}>{c}</span>
          </span>
        ))}
      </nav>
      <div className="ml-auto flex items-center gap-2">
        <button
          type="button"
          onClick={() => setPalette(true)}
          aria-label="Open command palette"
          className="flex h-7 items-center gap-2 whitespace-nowrap rounded-md border border-border bg-bg-inset px-2.5 text-xs text-fg-subtle hover:border-border-strong xl:w-60"
        >
          <Search className="h-3.5 w-3.5 shrink-0" />
          <span className="hidden flex-1 text-left xl:inline">Search or run a command…</span>
          <Kbd>{MOD}K</Kbd>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={isLight ? "Switch to dark theme" : "Switch to light theme"}
          title={isLight ? "Dark theme" : "Light theme"}
          onClick={() => void updatePrefs({ theme: isLight ? "dark" : "light" })}
        >
          {isLight ? <Moon className="h-4 w-4" /> : <Sun className="h-4 w-4" />}
          <span className="sr-only">{prefs.theme}</span>
        </Button>
        <EngineChip />
      </div>
    </header>
  );
}

function EngineChip() {
  const state = useAppStore((s) => s.engineState);
  const error = useAppStore((s) => s.engineError);
  const info = useAppStore((s) => s.engineInfo);
  const restart = useAppStore((s) => s.restartEngine);
  const [open, setOpen] = useState(false);
  const tone = state === "ready" ? "success" : state === "starting" ? "warning" : "danger";
  const label = { ready: "Engine ready", starting: "Engine starting", error: "Engine stopped", unavailable: "No engine" }[state];
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex h-7 items-center gap-2 whitespace-nowrap rounded-md border border-border px-2.5 text-xs text-fg-muted hover:bg-bg-hover"
        aria-label={`${label}. Show engine details`}
        title={label}
      >
        <StatusDot tone={tone} pulse={state === "starting"} />
        <span className="hidden lg:inline">{label}</span>
      </button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        size="sm"
        title="Engine"
        description="The local Relay engine process that talks to brokers and stores data."
        footer={
          <Button
            variant="secondary"
            disabled={state === "unavailable"}
            onClick={() => {
              void restart();
              setOpen(false);
            }}
          >
            <RotateCcw className="h-3.5 w-3.5" /> Restart engine
          </Button>
        }
      >
        <div className="space-y-3">
          <KeyValue
            items={[
              ["Status", <Badge tone={tone} dot>{label}</Badge>],
              ["Version", info?.engineVersion],
              ["Schema", info?.schemaVersion !== undefined ? `v${info.schemaVersion}` : undefined],
              [
                "Secret storage",
                info?.keystore === "os" ? (
                  "OS keychain"
                ) : info?.keystore === "file" ? (
                  <span className="text-warning">Local file fallback (OS keychain unavailable)</span>
                ) : undefined,
              ],
              ["Data directory", info?.dataDir ? <span className="font-mono text-[11px]">{info.dataDir}</span> : undefined],
            ]}
          />
          {error && <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</p>}
          <p className="text-[11px] text-fg-subtle">
            Restarting closes all broker connections and subscriptions. Saved connections and Insights history are kept.
          </p>
        </div>
      </Dialog>
    </>
  );
}
