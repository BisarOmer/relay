import { create } from "zustand";

export type ToastTone = "info" | "success" | "warning" | "danger";

export interface Toast {
  id: number;
  tone: ToastTone;
  title: string;
  description?: string;
  /** ms; 0 keeps the toast until dismissed */
  duration: number;
}

export interface ConfirmRequest {
  title: string;
  description?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: "default" | "danger";
  /** When set, the user must type this text to enable the confirm button. */
  requireText?: string;
}

interface PendingConfirm extends ConfirmRequest {
  resolve: (ok: boolean) => void;
}

interface UiState {
  toasts: Toast[];
  confirmRequest: PendingConfirm | null;
  shortcutsOpen: boolean;
  pushToast: (t: Omit<Toast, "id" | "duration"> & { duration?: number }) => number;
  dismissToast: (id: number) => void;
  requestConfirm: (req: ConfirmRequest) => Promise<boolean>;
  settleConfirm: (ok: boolean) => void;
  setShortcutsOpen: (open: boolean) => void;
}

let nextToastId = 1;

export const useUiStore = create<UiState>((set, get) => ({
  toasts: [],
  confirmRequest: null,
  shortcutsOpen: false,
  pushToast: (t) => {
    const id = nextToastId++;
    const duration = t.duration ?? (t.tone === "danger" ? 8000 : 4000);
    set((s) => ({ toasts: [...s.toasts.slice(-4), { ...t, id, duration }] }));
    return id;
  },
  dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  requestConfirm: (req) =>
    new Promise<boolean>((resolve) => {
      get().confirmRequest?.resolve(false);
      set({ confirmRequest: { ...req, resolve } });
    }),
  settleConfirm: (ok) => {
    const req = get().confirmRequest;
    set({ confirmRequest: null });
    req?.resolve(ok);
  },
  setShortcutsOpen: (shortcutsOpen) => set({ shortcutsOpen }),
}));

export const toast = {
  info: (title: string, description?: string) => useUiStore.getState().pushToast({ tone: "info", title, description }),
  success: (title: string, description?: string) =>
    useUiStore.getState().pushToast({ tone: "success", title, description }),
  warning: (title: string, description?: string) =>
    useUiStore.getState().pushToast({ tone: "warning", title, description }),
  error: (title: string, description?: string) =>
    useUiStore.getState().pushToast({ tone: "danger", title, description }),
};

export const confirm = (req: ConfirmRequest) => useUiStore.getState().requestConfirm(req);
