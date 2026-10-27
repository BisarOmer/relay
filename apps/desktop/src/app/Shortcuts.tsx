import { useEffect } from "react";
import { Dialog } from "@/components/ui/dialog";
import { Kbd } from "@/components/ui/misc";
import { useAppStore } from "@/stores/app-store";
import { useUiStore } from "@/stores/ui-store";
import { MOD, NAV_ITEMS } from "./navigation";

export const SHORTCUTS: { keys: string[]; label: string }[] = [
  { keys: [MOD, "K"], label: "Command palette" },
  { keys: [MOD, "B"], label: "Toggle sidebar" },
  { keys: [MOD, "J"], label: "Toggle output panel" },
  { keys: [MOD, "N"], label: "New connection" },
  ...NAV_ITEMS.map((n) => ({ keys: [MOD, n.shortcut], label: `Go to ${n.label}` })),
  { keys: ["?"], label: "Show keyboard shortcuts" },
  { keys: ["Esc"], label: "Close dialog or palette" },
];

function isTyping(target: EventTarget | null) {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName);
}

export function useGlobalShortcuts() {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const app = useAppStore.getState();
      const ui = useUiStore.getState();
      const mod = e.metaKey || e.ctrlKey;
      if (ui.confirmRequest) return;
      if (mod && e.key.toLowerCase() === "k") {
        e.preventDefault();
        app.setCommandPaletteOpen(!app.commandPaletteOpen);
        return;
      }
      if (app.commandPaletteOpen) return;
      if (mod && e.key.toLowerCase() === "b") {
        e.preventDefault();
        app.toggleSidebar();
        return;
      }
      if (mod && e.key.toLowerCase() === "j") {
        e.preventDefault();
        app.setBottomOpen(!app.bottomOpen);
        return;
      }
      if (mod && e.key.toLowerCase() === "n") {
        e.preventDefault();
        app.setActivity("explorer");
        app.openTab({ id: "new-connection", kind: "connection", title: "New connection", connectionId: "", mode: "new" });
        return;
      }
      if (mod && !e.shiftKey && !e.altKey) {
        const nav = NAV_ITEMS.find((n) => n.shortcut === e.key);
        if (nav) {
          e.preventDefault();
          app.setActivity(nav.id);
          return;
        }
      }
      if (e.key === "?" && !mod && !isTyping(e.target)) {
        e.preventDefault();
        ui.setShortcutsOpen(true);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

export function ShortcutsDialog() {
  const open = useUiStore((s) => s.shortcutsOpen);
  const setOpen = useUiStore((s) => s.setShortcutsOpen);
  return (
    <Dialog open={open} onClose={() => setOpen(false)} title="Keyboard shortcuts" size="sm">
      <ul className="divide-y divide-border">
        {SHORTCUTS.map((s) => (
          <li key={s.label} className="flex items-center justify-between py-2 text-[13px]">
            <span className="text-fg-muted">{s.label}</span>
            <span className="flex gap-1">
              {s.keys.map((k) => (
                <Kbd key={k}>{k}</Kbd>
              ))}
            </span>
          </li>
        ))}
      </ul>
    </Dialog>
  );
}
