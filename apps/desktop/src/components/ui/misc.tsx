import { Check, Copy, Loader2 } from "lucide-react";
import { useState } from "react";
import { cn, copyText } from "@/lib/utils";
import { toast } from "@/stores/ui-store";
import { Button } from "./button";

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
  compact,
}: {
  icon?: React.ComponentType<{ className?: string }>;
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
  compact?: boolean;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center text-center", compact ? "gap-2 p-6" : "gap-3 p-10", className)}>
      {Icon && (
        <div className="flex h-10 w-10 items-center justify-center rounded-lg border border-border bg-bg-elevated text-fg-subtle">
          <Icon className="h-5 w-5" />
        </div>
      )}
      <div className="max-w-sm space-y-1">
        <div className="text-[13px] font-medium text-fg">{title}</div>
        {description && <div className="text-xs leading-relaxed text-fg-muted">{description}</div>}
      </div>
      {action && <div className="mt-1 flex flex-wrap justify-center gap-2">{action}</div>}
    </div>
  );
}

export function Spinner({ className, label }: { className?: string; label?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 text-xs text-fg-muted", className)} role="status">
      <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
      {label ?? <span className="sr-only">Loading</span>}
    </span>
  );
}

export function Kbd({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-[18px] min-w-[18px] items-center justify-center rounded border border-border bg-bg-inset px-1 font-sans text-[10.5px] font-medium text-fg-subtle",
        className,
      )}
    >
      {children}
    </kbd>
  );
}

export function CopyButton({ text, label = "Copy", className, size = "xs" }: { text: string; label?: string; className?: string; size?: "xs" | "sm" }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      variant="ghost"
      size={size}
      className={className}
      aria-label={label}
      title={label}
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1200);
        } else {
          toast.error("Copy failed", "Clipboard access was denied.");
        }
      }}
    >
      {done ? <Check className="h-3 w-3 text-success" /> : <Copy className="h-3 w-3" />}
      {size === "sm" && label}
    </Button>
  );
}

export function SectionHeader({
  title,
  description,
  actions,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-end justify-between gap-3", className)}>
      <div className="min-w-0">
        <h3 className="text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-subtle">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-fg-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-1.5">{actions}</div>}
    </div>
  );
}

export function Card({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("rounded-lg border border-border bg-bg-panel", className)}>{children}</div>;
}

export function PageHeader({
  title,
  description,
  actions,
  meta,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  meta?: React.ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-start gap-3 border-b border-border px-5 py-3.5">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="truncate text-[15px] font-semibold tracking-tight">{title}</h1>
          {meta}
        </div>
        {description && <p className="mt-0.5 text-xs text-fg-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

/** A labelled value where unknown is visually distinct from zero. */
export function Stat({
  label,
  value,
  sub,
  tone,
  unknownReason,
  className,
  onClick,
}: {
  label: React.ReactNode;
  value: React.ReactNode | null;
  sub?: React.ReactNode;
  tone?: "default" | "warning" | "danger" | "success";
  unknownReason?: string;
  className?: string;
  onClick?: () => void;
}) {
  const unknown = value === null || value === undefined || value === "—";
  const Comp = onClick ? "button" : "div";
  return (
    <Comp
      type={onClick ? "button" : undefined}
      onClick={onClick}
      className={cn(
        "flex min-w-0 flex-col gap-1 rounded-lg border border-border bg-bg-panel px-3.5 py-3 text-left",
        onClick && "transition-colors hover:border-border-strong hover:bg-bg-hover",
        className,
      )}
    >
      <span className="truncate text-[11px] font-medium text-fg-subtle">{label}</span>
      <span
        title={unknown ? unknownReason ?? "Not reported in the latest snapshot" : undefined}
        className={cn(
          "tabular truncate text-xl font-semibold tracking-tight",
          unknown && "text-fg-subtle",
          tone === "warning" && !unknown && "text-warning",
          tone === "danger" && !unknown && "text-danger",
          tone === "success" && !unknown && "text-success",
        )}
      >
        {unknown ? "—" : value}
      </span>
      {(sub || unknown) && (
        <span className="truncate text-[11px] text-fg-subtle">{unknown ? unknownReason ?? "unavailable" : sub}</span>
      )}
    </Comp>
  );
}

export function KeyValue({ items, className }: { items: [React.ReactNode, React.ReactNode][]; className?: string }) {
  return (
    <dl className={cn("grid grid-cols-[minmax(0,max-content)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-xs", className)}>
      {items.map(([k, v], i) => (
        <div key={i} className="contents">
          <dt className="max-w-40 text-fg-subtle">{k}</dt>
          <dd className="min-w-0 break-words text-fg">{v ?? <span className="text-fg-subtle">—</span>}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Tabs<T extends string>({
  value,
  onChange,
  tabs,
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  tabs: { value: T; label: React.ReactNode; count?: number }[];
  className?: string;
}) {
  return (
    <div role="tablist" className={cn("flex items-center gap-0.5 border-b border-border px-2", className)}>
      {tabs.map((t) => (
        <button
          key={t.value}
          role="tab"
          type="button"
          aria-selected={value === t.value}
          onClick={() => onChange(t.value)}
          className={cn(
            "-mb-px flex h-8 items-center gap-1.5 border-b-2 px-2.5 text-xs font-medium transition-colors",
            value === t.value ? "border-accent text-fg" : "border-transparent text-fg-muted hover:text-fg",
          )}
        >
          {t.label}
          {t.count !== undefined && (
            <span className="tabular rounded bg-bg-hover px-1 text-[10px] text-fg-subtle">{t.count}</span>
          )}
        </button>
      ))}
    </div>
  );
}

export function Banner({
  tone = "info",
  children,
  action,
  className,
}: {
  tone?: "info" | "warning" | "danger" | "production";
  children: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      role={tone === "danger" ? "alert" : undefined}
      className={cn(
        "flex items-center gap-3 border px-3 py-2 text-xs",
        tone === "info" && "border-info/30 bg-info/8 text-info",
        tone === "warning" && "border-warning/30 bg-warning/10 text-warning",
        tone === "danger" && "border-danger/30 bg-danger/10 text-danger",
        tone === "production" && "border-production/35 bg-production/10 text-production",
        className,
      )}
    >
      <div className="min-w-0 flex-1">{children}</div>
      {action}
    </div>
  );
}
