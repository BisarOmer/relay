import { CornerDownLeft, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { MOD, NAV_ITEMS } from "@/app/navigation";
import { Kbd } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { useUiStore } from "@/stores/ui-store";

interface Command {
  id: string;
  group: string;
  label: string;
  hint?: string;
  keywords?: string;
  run: () => void;
}

/** Simple subsequence match; lower score is better. */
export function fuzzyScore(query: string, text: string): number | null {
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  if (!q) return 0;
  const idx = t.indexOf(q);
  if (idx >= 0) return idx;
  let ti = 0;
  let gaps = 0;
  for (const ch of q) {
    const found = t.indexOf(ch, ti);
    if (found < 0) return null;
    gaps += found - ti;
    ti = found + 1;
  }
  return 100 + gaps;
}

export function CommandPalette() {
  const open = useAppStore((s) => s.commandPaletteOpen);
  const setOpen = useAppStore((s) => s.setCommandPaletteOpen);
  const connections = useAppStore((s) => s.connections);
  const runtimeStatus = useAppStore((s) => s.runtimeStatus);
  const engineState = useAppStore((s) => s.engineState);
  const theme = useAppStore((s) => s.preferences.theme);
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  const commands = useMemo<Command[]>(() => {
    const app = useAppStore.getState();
    const list: Command[] = NAV_ITEMS.map((n) => ({
      id: `nav-${n.id}`,
      group: "Go to",
      label: n.label,
      hint: `${MOD}${n.shortcut}`,
      keywords: n.description,
      run: () => app.setActivity(n.id),
    }));
    list.push(
      {
        id: "new-connection",
        group: "Actions",
        label: "New connection",
        hint: `${MOD}N`,
        keywords: "add nats server",
        run: () => {
          app.setActivity("explorer");
          app.openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" });
        },
      },
      {
        id: "toggle-theme",
        group: "Actions",
        label: theme === "light" ? "Switch to dark theme" : "Switch to light theme",
        keywords: "appearance color mode",
        run: () => void app.updatePreferences({ theme: theme === "light" ? "dark" : "light" }),
      },
      { id: "toggle-sidebar", group: "Actions", label: "Toggle sidebar", hint: `${MOD}B`, run: () => app.toggleSidebar() },
      { id: "toggle-output", group: "Actions", label: "Toggle output panel", hint: `${MOD}J`, run: () => app.setBottomOpen(!app.bottomOpen) },
      { id: "shortcuts", group: "Actions", label: "Keyboard shortcuts", hint: "?", run: () => useUiStore.getState().setShortcutsOpen(true) },
    );
    if (engineState !== "unavailable") {
      list.push({ id: "restart-engine", group: "Actions", label: "Restart engine", keywords: "process sidecar", run: () => void app.restartEngine() });
    }
    for (const c of connections) {
      const st = runtimeStatus[c.id] ?? c.lastStatus;
      const live = st === "connected" || st === "reconnecting" || st === "connecting";
      list.push({
        id: `open-${c.id}`,
        group: "Connections",
        label: c.name,
        hint: st,
        keywords: c.servers.join(" "),
        run: () => {
          app.setActivity("explorer");
          app.openTab({ id: `conn-${c.id}`, kind: "connection", title: c.name, connectionId: c.id });
        },
      });
      list.push({
        id: `toggle-${c.id}`,
        group: "Connections",
        label: `${live ? "Disconnect" : "Connect"} ${c.name}`,
        run: () => void (live ? app.disconnect(c.id) : app.connect(c.id)),
      });
      if (st === "connected") {
        list.push({
          id: `live-${c.id}`,
          group: "Connections",
          label: `Live messages on ${c.name}`,
          run: () => {
            app.openTab({ id: `msgs-${c.id}`, kind: "messages", title: `${c.name} · live`, connectionId: c.id });
          },
        });
      }
    }
    return list;
  }, [connections, runtimeStatus, engineState, theme]);

  const filtered = useMemo(() => {
    const q = query.trim();
    if (!q) return commands;
    return commands
      .map((c) => ({ c, s: Math.min(fuzzyScore(q, c.label) ?? Infinity, (fuzzyScore(q, c.keywords ?? "") ?? Infinity) + 50) }))
      .filter((x) => x.s !== Infinity)
      .sort((a, b) => a.s - b.s)
      .map((x) => x.c);
  }, [commands, query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setIndex(0);
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  useEffect(() => setIndex(0), [query]);

  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${index}"]`)?.scrollIntoView({ block: "nearest" });
  }, [index]);

  if (!open) return null;

  const runAt = (i: number) => {
    const cmd = filtered[i];
    if (!cmd) return;
    setOpen(false);
    cmd.run();
  };

  let lastGroup = "";
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/45 pt-[12vh] animate-fade-in" onMouseDown={() => setOpen(false)}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        className="w-[560px] max-w-[92vw] overflow-hidden rounded-xl border border-border bg-bg-elevated shadow-overlay animate-slide-up"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Search className="h-4 w-4 text-fg-subtle" />
          <input
            ref={inputRef}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-activedescendant={filtered[index] ? `palette-${filtered[index].id}` : undefined}
            className="h-11 flex-1 bg-transparent text-[13px] outline-none placeholder:text-fg-subtle"
            placeholder="Type a command or connection name…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setIndex((i) => Math.min(i + 1, filtered.length - 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setIndex((i) => Math.max(i - 1, 0));
              } else if (e.key === "Enter") {
                e.preventDefault();
                runAt(index);
              } else if (e.key === "Escape") {
                e.preventDefault();
                setOpen(false);
              }
            }}
          />
          <Kbd>Esc</Kbd>
        </div>
        <ul id="palette-list" ref={listRef} role="listbox" className="max-h-[360px] overflow-auto p-1.5">
          {filtered.length === 0 && <li className="px-3 py-6 text-center text-xs text-fg-subtle">No matching commands</li>}
          {filtered.map((cmd, i) => {
            const header = !query && cmd.group !== lastGroup ? cmd.group : null;
            lastGroup = cmd.group;
            return (
              <li key={cmd.id} role="presentation">
                {header && <div className="px-2.5 pb-1 pt-2 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-fg-subtle">{header}</div>}
                <button
                  type="button"
                  id={`palette-${cmd.id}`}
                  role="option"
                  aria-selected={i === index}
                  data-index={i}
                  className={cn("flex h-8 w-full items-center gap-2 rounded-md px-2.5 text-left text-[13px]", i === index ? "bg-accent/15 text-fg" : "text-fg-muted")}
                  onMouseMove={() => setIndex(i)}
                  onClick={() => runAt(i)}
                >
                  <span className="flex-1 truncate">{cmd.label}</span>
                  {query && <span className="text-[11px] text-fg-subtle">{cmd.group}</span>}
                  {cmd.hint && <Kbd>{cmd.hint}</Kbd>}
                  {i === index && <CornerDownLeft className="h-3 w-3 text-fg-subtle" />}
                </button>
              </li>
            );
          })}
        </ul>
      </div>
    </div>,
    document.body,
  );
}
