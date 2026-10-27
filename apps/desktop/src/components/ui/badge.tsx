import { cn } from "@/lib/utils";

export type Tone = "neutral" | "success" | "warning" | "danger" | "accent" | "info" | "production";

const tones: Record<Tone, string> = {
  neutral: "bg-bg-hover text-fg-muted border-border",
  success: "bg-success/12 text-success border-success/30",
  warning: "bg-warning/12 text-warning border-warning/30",
  danger: "bg-danger/12 text-danger border-danger/30",
  accent: "bg-accent/12 text-accent border-accent/30",
  info: "bg-info/12 text-info border-info/30",
  production: "bg-production/15 text-production border-production/40",
};

const dots: Record<Tone, string> = {
  neutral: "bg-fg-subtle",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
  accent: "bg-accent",
  info: "bg-info",
  production: "bg-production",
};

export function Badge({
  children,
  tone = "neutral",
  className,
  dot,
  title,
}: {
  children: React.ReactNode;
  tone?: Tone;
  className?: string;
  dot?: boolean;
  title?: string;
}) {
  return (
    <span
      title={title}
      className={cn(
        "inline-flex h-[18px] shrink-0 items-center gap-1 whitespace-nowrap rounded border px-1.5 text-[10.5px] font-medium leading-none",
        tones[tone],
        className,
      )}
    >
      {dot && <span className={cn("h-1.5 w-1.5 rounded-full", dots[tone])} aria-hidden />}
      {children}
    </span>
  );
}

export function StatusDot({ tone, pulse, className, label }: { tone: Tone; pulse?: boolean; className?: string; label?: string }) {
  return (
    <span className={cn("relative inline-flex h-2 w-2 shrink-0", className)} role={label ? "img" : undefined} aria-label={label}>
      {pulse && <span className={cn("absolute inset-0 animate-ping rounded-full opacity-60", dots[tone])} />}
      <span className={cn("relative h-2 w-2 rounded-full", dots[tone])} />
    </span>
  );
}

export function connectionTone(status: string | undefined): Tone {
  switch (status) {
    case "connected":
      return "success";
    case "connecting":
    case "reconnecting":
    case "disconnecting":
      return "warning";
    case "error":
      return "danger";
    default:
      return "neutral";
  }
}

export function healthTone(status: string | undefined): Tone {
  switch (status) {
    case "ok":
      return "success";
    case "partial":
    case "degraded":
    case "collecting":
    case "warning":
      return "warning";
    case "unreachable":
    case "unhealthy":
    case "error":
    case "critical":
      return "danger";
    case "info":
      return "info";
    default:
      return "neutral";
  }
}

export function severityTone(sev: string): Tone {
  return sev === "critical" ? "danger" : sev === "warning" ? "warning" : "info";
}
