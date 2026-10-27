import { Activity, Box, Radar, Radio, Send, Settings, type LucideIcon } from "lucide-react";
import type { ActivityView } from "@/types/models";

export interface NavItem {
  id: ActivityView;
  icon: LucideIcon;
  label: string;
  description: string;
  shortcut: string;
}

/** Every entry here leads to an implemented page. */
export const NAV_ITEMS: NavItem[] = [
  { id: "explorer", icon: Box, label: "Connections", description: "Broker connections and JetStream streams", shortcut: "1" },
  { id: "messages", icon: Radio, label: "Live messages", description: "Subscribe and inspect traffic", shortcut: "2" },
  { id: "publisher", icon: Send, label: "Publisher", description: "Publish and request/reply", shortcut: "3" },
  { id: "insights", icon: Radar, label: "Insights", description: "Server monitoring and diagnostics", shortcut: "4" },
  { id: "monitoring", icon: Activity, label: "Session activity", description: "This app's connections and subscriptions", shortcut: "5" },
  { id: "settings", icon: Settings, label: "Settings", description: "Appearance and engine", shortcut: "," },
];

export const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
export const MOD = isMac ? "⌘" : "Ctrl";
