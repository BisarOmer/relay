import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { Kbd } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { MOD, NAV_ITEMS } from "./navigation";

export function Sidebar() {
  const activity = useAppStore((s) => s.activity);
  const setActivity = useAppStore((s) => s.setActivity);
  const collapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggle = useAppStore((s) => s.toggleSidebar);
  const activeSubscriptions = useAppStore((s) => s.subscriptions.length);

  return (
    <nav
      aria-label="Primary"
      className={cn(
        "flex shrink-0 flex-col border-r border-border bg-bg-panel transition-[width] duration-150",
        collapsed ? "w-12" : "w-52",
      )}
    >
      <div className={cn("flex h-11 items-center border-b border-border", collapsed ? "justify-center" : "gap-2 px-3")}>
        <div className="flex h-6 w-6 items-center justify-center rounded-md bg-accent text-[12px] font-bold text-accent-fg" aria-hidden>
          R
        </div>
        {!collapsed && (
          <div className="min-w-0 leading-tight">
            <div className="text-[13px] font-semibold tracking-tight">Relay</div>
            <div className="text-[10.5px] text-fg-subtle">Broker IDE</div>
          </div>
        )}
      </div>
      <ul className="flex-1 space-y-0.5 p-1.5">
        {NAV_ITEMS.map(({ id, icon: Icon, label, shortcut }) => {
          const active = activity === id;
          return (
            <li key={id}>
              <button
                type="button"
                title={collapsed ? `${label} (${MOD}${shortcut})` : undefined}
                aria-label={label}
                aria-current={active ? "page" : undefined}
                onClick={() => setActivity(id)}
                className={cn(
                  "group relative flex h-8 w-full items-center gap-2.5 rounded-md text-[13px] transition-colors",
                  collapsed ? "justify-center" : "px-2.5",
                  active ? "bg-bg-active text-fg" : "text-fg-muted hover:bg-bg-hover hover:text-fg",
                )}
              >
                {active && <span className="absolute left-0 top-1.5 h-5 w-0.5 rounded-r bg-accent" aria-hidden />}
                <Icon className={cn("h-4 w-4", active && "text-accent")} />
                {!collapsed && (
                  <>
                    <span className="flex-1 truncate text-left">{label}</span>
                    {id === "monitoring" && activeSubscriptions > 0 && (
                      <span className="tabular rounded bg-bg-hover px-1 text-[10px] text-fg-subtle">{activeSubscriptions}</span>
                    )}
                    <Kbd className="opacity-0 group-hover:opacity-100">
                      {MOD}
                      {shortcut}
                    </Kbd>
                  </>
                )}
              </button>
            </li>
          );
        })}
      </ul>
      <div className="border-t border-border p-1.5">
        <button
          type="button"
          onClick={toggle}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          title={`${collapsed ? "Expand" : "Collapse"} sidebar (${MOD}B)`}
          className={cn(
            "flex h-8 w-full items-center gap-2.5 rounded-md text-xs text-fg-subtle hover:bg-bg-hover hover:text-fg",
            collapsed ? "justify-center" : "px-2.5",
          )}
        >
          {collapsed ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
          {!collapsed && "Collapse"}
        </button>
      </div>
    </nav>
  );
}
