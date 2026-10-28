import { X } from "lucide-react";
import { useEffect, useId, useRef } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";
import { Button } from "./button";

const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

function firstContentFocusable(panel: HTMLElement): HTMLElement | null {
  const preferred = panel.querySelector<HTMLElement>("[autofocus], [data-autofocus]");
  if (preferred) return preferred;
  const items = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE));
  return items.find((el) => !el.hasAttribute("data-dialog-close")) ?? items[0] ?? null;
}

/**
 * Accessible modal: focus is moved inside, trapped while open, and restored
 * to the previously focused element on close. Escape and backdrop close it.
 */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = "md",
  className,
  initialFocus,
}: {
  open: boolean;
  onClose: () => void;
  title: React.ReactNode;
  description?: React.ReactNode;
  children?: React.ReactNode;
  footer?: React.ReactNode;
  size?: "sm" | "md" | "lg";
  className?: string;
  initialFocus?: React.RefObject<HTMLElement | null>;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    const target = initialFocus?.current ?? (panel ? firstContentFocusable(panel) : null) ?? panel;
    target?.focus();
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.stopPropagation();
        onCloseRef.current();
      }
      if (e.key === "Tab" && panel) {
        const items = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE));
        if (items.length === 0) return;
        const first = items[0];
        const last = items[items.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    }
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      previous?.focus?.();
    };
    // Intentionally omit onClose: callers often pass an inline function, and
    // re-running this effect would steal focus back to the dialog chrome.
  }, [open, initialFocus]);

  if (!open) return null;
  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/45 p-4 pt-[12vh] animate-fade-in"
      onMouseDown={() => onCloseRef.current()}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descId : undefined}
        tabIndex={-1}
        onMouseDown={(e) => e.stopPropagation()}
        className={cn(
          "flex max-h-[76vh] w-full flex-col overflow-hidden rounded-lg border border-border bg-bg-elevated shadow-[var(--shadow-overlay)] animate-slide-up focus:outline-none",
          size === "sm" && "max-w-md",
          size === "md" && "max-w-xl",
          size === "lg" && "max-w-3xl",
          className,
        )}
      >
        <div className="flex items-start gap-3 border-b border-border px-5 py-3.5">
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="text-[15px] font-semibold tracking-tight">
              {title}
            </h2>
            {description && (
              <div id={descId} className="mt-0.5 text-xs text-fg-muted">
                {description}
              </div>
            )}
          </div>
          <Button data-dialog-close variant="ghost" size="icon-sm" aria-label="Close" onClick={() => onCloseRef.current()}>
            <X className="h-4 w-4" />
          </Button>
        </div>
        {children && <div className="min-h-0 flex-1 overflow-auto px-5 py-4">{children}</div>}
        {footer && <div className="flex justify-end gap-2 border-t border-border bg-bg-panel px-5 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
