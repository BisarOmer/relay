import { create } from "zustand";
import {
  engineRequest,
  engineRestart,
  engineStatus,
  errorMessage,
  onEngineEvent,
  transportAvailable,
  type EngineHostStatus,
} from "@/lib/engine";
import { toast } from "@/stores/ui-store";
import {
  DEFAULT_PREFERENCES,
  type ActivityView,
  type ConnectionProfile,
  type ConnectionStatus,
  type InboundMessage,
  type Preferences,
  type SubscriptionStatus,
  type WorkspaceTab,
} from "@/types/models";

export type EngineState = "starting" | "ready" | "error" | "unavailable";

export interface LogLine {
  at: string;
  level: "info" | "warn" | "error";
  text: string;
}

type Listener = (ev: { event: string; payload?: unknown }) => void;

interface AppState {
  engineState: EngineState;
  engineError: string | null;
  engineInfo: EngineHostStatus["hello"];
  preferences: Preferences;
  activity: ActivityView;
  sidebarCollapsed: boolean;
  connections: ConnectionProfile[];
  connectionsLoaded: boolean;
  runtimeStatus: Record<string, ConnectionStatus>;
  statusDetail: Record<string, string>;
  selectedConnectionId: string | null;
  tabs: WorkspaceTab[];
  activeTabId: string;
  messages: InboundMessage[];
  selectedMessageId: string | null;
  subscriptions: SubscriptionStatus[];
  bottomOpen: boolean;
  bottomLog: LogLine[];
  commandPaletteOpen: boolean;
  followLatest: boolean;
  listeners: Set<Listener>;
  /** Extra breadcrumb segments contributed by the active page. */
  crumbs: string[];

  setCrumbs: (crumbs: string[]) => void;
  setActivity: (v: ActivityView) => void;
  toggleSidebar: () => void;
  setCommandPaletteOpen: (open: boolean) => void;
  setBottomOpen: (open: boolean) => void;
  appendLog: (text: string, level?: LogLine["level"]) => void;
  clearLog: () => void;
  openTab: (tab: WorkspaceTab) => void;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  selectConnection: (id: string | null) => void;
  refreshConnections: () => Promise<void>;
  connect: (id: string) => Promise<boolean>;
  disconnect: (id: string) => Promise<void>;
  bootstrap: () => Promise<() => void>;
  restartEngine: () => Promise<void>;
  updatePreferences: (patch: Partial<Preferences>) => Promise<void>;
  clearMessages: (connectionId?: string) => void;
  setSelectedMessage: (id: string | null) => void;
  setFollowLatest: (v: boolean) => void;
  /** Subscribe to raw engine events (used by feature workspaces). */
  subscribeEvents: (fn: Listener) => () => void;
}

const welcomeTab: WorkspaceTab = { id: "welcome", kind: "welcome", title: "Welcome" };

/** Keeps the highlighted activity in step with the tab being shown. */
function activityPatch(s: Pick<AppState, "activity" | "crumbs">, tab: WorkspaceTab) {
  const activity: ActivityView =
    tab.kind === "messages" ? "messages" : tab.kind === "publisher" ? "publisher" : "explorer";
  return activity === s.activity ? {} : { activity, crumbs: [] };
}

function systemPrefersLight() {
  return typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: light)").matches;
}

export function applyPreferences(p: Preferences) {
  const root = document.documentElement;
  const theme = p.theme === "system" ? (systemPrefersLight() ? "light" : "dark") : p.theme;
  root.dataset.theme = theme;
  root.dataset.density = p.density;
}

export const useAppStore = create<AppState>((set, get) => ({
  engineState: "starting",
  engineError: null,
  engineInfo: null,
  preferences: DEFAULT_PREFERENCES,
  activity: "explorer",
  sidebarCollapsed: false,
  connections: [],
  connectionsLoaded: false,
  runtimeStatus: {},
  statusDetail: {},
  selectedConnectionId: null,
  tabs: [welcomeTab],
  activeTabId: "welcome",
  messages: [],
  selectedMessageId: null,
  subscriptions: [],
  bottomOpen: false,
  bottomLog: [],
  commandPaletteOpen: false,
  followLatest: true,
  listeners: new Set(),
  crumbs: [],

  setCrumbs: (crumbs) => {
    const cur = get().crumbs;
    if (cur.length === crumbs.length && cur.every((c, i) => c === crumbs[i])) return;
    set({ crumbs });
  },
  setActivity: (activity) => {
    set({ activity, crumbs: [] });
    const { tabs, activeTabId } = get();
    const kind = activity === "messages" ? "messages" : activity === "publisher" ? "publisher" : null;
    if (!kind) return;
    const current = tabs.find((t) => t.id === activeTabId);
    if (current?.kind === kind) return;
    const existing = [...tabs].reverse().find((t) => t.kind === kind);
    if (existing) {
      set({ activeTabId: existing.id });
    } else if (kind === "publisher") {
      get().openTab({ id: "publisher", kind: "publisher", title: "Publisher" });
    } else {
      get().openTab({ id: "messages-home", kind: "messages", title: "Live messages", connectionId: "" });
    }
  },
  toggleSidebar: () => {
    const next = !get().sidebarCollapsed;
    set({ sidebarCollapsed: next });
    void get().updatePreferences({ sidebarCollapsed: next });
  },
  setCommandPaletteOpen: (commandPaletteOpen) => set({ commandPaletteOpen }),
  setBottomOpen: (bottomOpen) => set({ bottomOpen }),
  appendLog: (text, level = "info") =>
    set((s) => ({ bottomLog: [...s.bottomLog.slice(-499), { at: new Date().toISOString(), level, text }] })),
  clearLog: () => set({ bottomLog: [] }),
  openTab: (tab) =>
    set((s) => {
      const existing = s.tabs.find((t) => t.id === tab.id);
      const tabs = existing ? s.tabs.map((t) => (t.id === tab.id ? tab : t)) : [...s.tabs.filter((t) => t.kind !== "welcome"), tab];
      return { tabs, activeTabId: tab.id, ...activityPatch(s, tab) };
    }),
  closeTab: (id) =>
    set((s) => {
      const idx = s.tabs.findIndex((t) => t.id === id);
      const tabs = s.tabs.filter((t) => t.id !== id);
      if (tabs.length === 0) return { tabs: [welcomeTab], activeTabId: "welcome" };
      if (s.activeTabId !== id) return { tabs };
      const next = tabs[Math.max(0, idx - 1)];
      return { tabs, activeTabId: next.id, ...activityPatch(s, next) };
    }),
  setActiveTab: (activeTabId) =>
    set((s) => {
      const tab = s.tabs.find((t) => t.id === activeTabId);
      return tab ? { activeTabId, ...activityPatch(s, tab) } : {};
    }),
  selectConnection: (selectedConnectionId) => set({ selectedConnectionId }),
  clearMessages: (connectionId) =>
    set((s) => ({
      messages: connectionId ? s.messages.filter((m) => m.connectionId !== connectionId) : [],
      selectedMessageId: null,
    })),
  setSelectedMessage: (selectedMessageId) => set({ selectedMessageId, followLatest: selectedMessageId === null }),
  setFollowLatest: (followLatest) => set({ followLatest }),
  subscribeEvents: (fn) => {
    get().listeners.add(fn);
    return () => get().listeners.delete(fn);
  },

  refreshConnections: async () => {
    const res = await engineRequest<{ connections: ConnectionProfile[] }>("connections.list");
    const list = res.connections ?? [];
    set((s) => {
      const runtimeStatus = { ...s.runtimeStatus };
      for (const c of list) runtimeStatus[c.id] = c.lastStatus;
      return { connections: list, connectionsLoaded: true, runtimeStatus };
    });
  },

  connect: async (id) => {
    const c = get().connections.find((x) => x.id === id);
    try {
      await engineRequest("connections.connect", { id }, (c?.timeoutMs ?? 10_000) + 10_000);
      return true;
    } catch (e) {
      const msg = errorMessage(e);
      set((s) => ({ statusDetail: { ...s.statusDetail, [id]: msg } }));
      toast.error(`Could not connect${c ? ` to ${c.name}` : ""}`, msg);
      get().appendLog(`Connect failed (${c?.name ?? id}): ${msg}`, "error");
      return false;
    }
  },

  disconnect: async (id) => {
    try {
      await engineRequest("connections.disconnect", { id });
    } catch (e) {
      toast.error("Disconnect failed", errorMessage(e));
    }
  },

  updatePreferences: async (patch) => {
    const prev = get().preferences;
    const optimistic = { ...prev, ...patch };
    set({ preferences: optimistic });
    applyPreferences(optimistic);
    try {
      const saved = await engineRequest<Preferences>("preferences.set", patch);
      set({ preferences: saved });
      applyPreferences(saved);
    } catch (e) {
      set({ preferences: prev });
      applyPreferences(prev);
      toast.error("Could not save preference", errorMessage(e));
    }
  },

  restartEngine: async () => {
    set({ engineState: "starting", engineError: null });
    try {
      await engineRestart();
    } catch (e) {
      set({ engineState: "error", engineError: errorMessage(e) });
    }
  },

  bootstrap: async () => {
    applyPreferences(get().preferences);
    const media = window.matchMedia?.("(prefers-color-scheme: light)");
    const onScheme = () => {
      if (get().preferences.theme === "system") applyPreferences(get().preferences);
    };
    media?.addEventListener?.("change", onScheme);

    if (!transportAvailable()) {
      set({
        engineState: "unavailable",
        engineError: "No engine transport. Run the desktop app (npm run tauri dev) or start the dev bridge.",
      });
      return () => media?.removeEventListener?.("change", onScheme);
    }

    const onReady = async () => {
      set({ engineState: "ready", engineError: null });
      try {
        const st = await engineStatus();
        set({ engineInfo: st.hello ?? null });
        if (st.hello?.storageRecovered) {
          toast.warning(
            "Local database was recovered",
            "The previous database could not be read and was moved aside. Saved connections may need to be re-created.",
          );
        }
      } catch {
        // status is informational
      }
      try {
        const prefs = await engineRequest<Preferences>("preferences.get");
        set({ preferences: prefs, sidebarCollapsed: prefs.sidebarCollapsed });
        applyPreferences(prefs);
      } catch (e) {
        get().appendLog(`Preferences unavailable: ${errorMessage(e)}`, "warn");
      }
      try {
        await get().refreshConnections();
        const subs = await engineRequest<{ subscriptions: SubscriptionStatus[] }>("messages.subscriptions");
        set({ subscriptions: subs.subscriptions ?? [] });
      } catch (e) {
        get().appendLog(`Failed to load connections: ${errorMessage(e)}`, "error");
      }
    };

    const unlisten = await onEngineEvent((ev) => {
      const event = ev.event;
      for (const l of get().listeners) l(ev);
      switch (event) {
        case "engine.ready":
          get().appendLog("Engine ready");
          void onReady();
          break;
        case "engine.error":
        case "engine.exited": {
          const payload = ev.payload as { message?: string } | undefined;
          const msg = payload?.message ?? "Engine error";
          set((s) => ({
            engineState: "error",
            engineError: msg,
            subscriptions: [],
            runtimeStatus: Object.fromEntries(Object.keys(s.runtimeStatus).map((k) => [k, "disconnected" as const])),
          }));
          get().appendLog(msg, "error");
          break;
        }
        case "connection.status": {
          const p = ev.payload as { connectionId: string; status: ConnectionStatus; detail?: string };
          set((s) => ({
            runtimeStatus: { ...s.runtimeStatus, [p.connectionId]: p.status },
            statusDetail: { ...s.statusDetail, [p.connectionId]: p.detail ?? "" },
          }));
          const name = get().connections.find((c) => c.id === p.connectionId)?.name ?? p.connectionId.slice(0, 8);
          const level = p.status === "error" ? "error" : p.status === "reconnecting" ? "warn" : "info";
          get().appendLog(`${name}: ${p.status}${p.detail ? ` — ${p.detail}` : ""}`, level);
          if (p.status === "reconnecting") toast.warning(`${name} lost its connection`, "Reconnecting automatically…");
          if (p.status === "error" && p.detail) toast.error(`${name} disconnected`, p.detail);
          break;
        }
        case "messages.inbound": {
          const payload = ev.payload as { messages: InboundMessage[] };
          const max = get().preferences.maxDisplayedMessages;
          set((s) => {
            const merged = s.messages.concat(payload.messages ?? []);
            const trimmed = merged.length > max ? merged.slice(merged.length - max) : merged;
            const counts = new Map<string, number>();
            for (const m of payload.messages ?? []) {
              if (m.subscriptionId) counts.set(m.subscriptionId, (counts.get(m.subscriptionId) ?? 0) + 1);
            }
            return {
              messages: trimmed,
              selectedMessageId: s.followLatest ? (trimmed.at(-1)?.id ?? null) : s.selectedMessageId,
              subscriptions: counts.size
                ? s.subscriptions.map((x) => (counts.has(x.id) ? { ...x, messageCount: x.messageCount + counts.get(x.id)! } : x))
                : s.subscriptions,
            };
          });
          break;
        }
        case "messages.dropped": {
          const p = ev.payload as { subscriptionId: string; droppedCount: number; reason?: string };
          set((s) => ({
            subscriptions: s.subscriptions.map((x) =>
              x.id === p.subscriptionId ? { ...x, droppedCount: p.droppedCount } : x,
            ),
          }));
          break;
        }
        case "subscription.status": {
          const st = ev.payload as SubscriptionStatus;
          set((s) => {
            const others = s.subscriptions.filter((x) => x.id !== st.id);
            return { subscriptions: st.status === "stopped" ? others : [...others, st] };
          });
          break;
        }
      }
    });

    try {
      const st = await engineStatus();
      if (st.ready) await onReady();
      else if (st.lastError) set({ engineState: "error", engineError: st.lastError });
    } catch {
      // the engine.ready event will arrive once the host finishes starting it
    }

    return () => {
      unlisten();
      media?.removeEventListener?.("change", onScheme);
    };
  },
}));
