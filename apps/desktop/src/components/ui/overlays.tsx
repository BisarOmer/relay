import { AlertTriangle, CheckCircle2, Info, X, XCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";
import { useUiStore, type Toast } from "@/stores/ui-store";
import { Button } from "./button";
import { Dialog } from "./dialog";
import { Input } from "./input";

export function ConfirmHost() {
  const req = useUiStore((s) => s.confirmRequest);
  const settle = useUiStore((s) => s.settleConfirm);
  const [typed, setTyped] = useState("");
  const cancelRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => setTyped(""), [req]);

  const blocked = !!req?.requireText && typed !== req.requireText;
  return (
    <Dialog
      open={!!req}
      onClose={() => settle(false)}
      size="sm"
      title={req?.title}
      initialFocus={req?.requireText ? inputRef : cancelRef}
      footer={
        <>
          <Button ref={cancelRef} variant="secondary" onClick={() => settle(false)}>
            {req?.cancelLabel ?? "Cancel"}
          </Button>
          <Button
            variant={req?.tone === "danger" ? "danger" : "default"}
            disabled={blocked}
            onClick={() => settle(true)}
          >
            {req?.confirmLabel ?? "Confirm"}
          </Button>
        </>
      }
    >
      <div className="space-y-3 text-[13px] text-fg-muted">
        {req?.description}
        {req?.requireText && (
          <div className="grid gap-1">
            <label className="text-xs" htmlFor="confirm-text">
              Type <span className="font-mono font-semibold text-fg">{req.requireText}</span> to confirm
            </label>
            <Input
              id="confirm-text"
              ref={inputRef}
              value={typed}
              autoComplete="off"
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !blocked) settle(true);
              }}
            />
          </div>
        )}
      </div>
    </Dialog>
  );
}

const toastIcon = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: XCircle,
};

const toastTone = {
  info: "text-info",
  success: "text-success",
  warning: "text-warning",
  danger: "text-danger",
};

function ToastItem({ t }: { t: Toast }) {
  const dismiss = useUiStore((s) => s.dismissToast);
  const [hover, setHover] = useState(false);
  useEffect(() => {
    if (!t.duration || hover) return;
    const h = setTimeout(() => dismiss(t.id), t.duration);
    return () => clearTimeout(h);
  }, [t, hover, dismiss]);
  const Icon = toastIcon[t.tone];
  return (
    <div
      role={t.tone === "danger" ? "alert" : "status"}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      className="pointer-events-auto flex w-80 items-start gap-2.5 rounded-lg border border-border bg-bg-elevated px-3 py-2.5 shadow-[var(--shadow-overlay)] animate-slide-up"
    >
      <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", toastTone[t.tone])} aria-hidden />
      <div className="min-w-0 flex-1">
        <div className="text-[13px] font-medium">{t.title}</div>
        {t.description && <div className="mt-0.5 break-words text-xs text-fg-muted">{t.description}</div>}
      </div>
      <button
        type="button"
        aria-label="Dismiss notification"
        className="rounded p-0.5 text-fg-subtle hover:bg-bg-hover hover:text-fg"
        onClick={() => dismiss(t.id)}
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

export function Toaster() {
  const toasts = useUiStore((s) => s.toasts);
  return createPortal(
    <div className="pointer-events-none fixed bottom-10 right-4 z-[60] flex flex-col items-end gap-2" aria-live="polite">
      {toasts.map((t) => (
        <ToastItem key={t.id} t={t} />
      ))}
    </div>,
    document.body,
  );
}
