import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

export type EngineErrorCode =
  | "validation_error"
  | "auth_error"
  | "authorization_error"
  | "network_error"
  | "tls_error"
  | "broker_error"
  | "timeout"
  | "unsupported"
  | "not_found"
  | "limit_exceeded"
  | "cancelled"
  | "internal"
  | "engine_unavailable"
  | (string & {});

export class EngineError extends Error {
  readonly code: EngineErrorCode;
  readonly retryable: boolean;
  readonly details?: unknown;

  constructor(code: EngineErrorCode, message: string, retryable = false, details?: unknown) {
    super(message);
    this.name = "EngineError";
    this.code = code;
    this.retryable = retryable;
    this.details = details;
  }

  static from(err: unknown): EngineError {
    if (err instanceof EngineError) return err;
    if (err && typeof err === "object" && "code" in err && "message" in err) {
      const e = err as { code: string; message: string; retryable?: boolean; details?: unknown };
      return new EngineError(e.code, e.message, !!e.retryable, e.details);
    }
    if (typeof err === "string") {
      const m = /^([a-z_]+): (.*)$/s.exec(err);
      if (m) return new EngineError(m[1], m[2]);
      return new EngineError("internal", err);
    }
    if (err instanceof Error) return new EngineError("internal", err.message);
    return new EngineError("internal", "Unknown error");
  }
}

/** Human-readable message for any thrown value. */
export function errorMessage(err: unknown): string {
  return EngineError.from(err).message;
}

export type EngineEvent = {
  type?: string;
  event: string;
  correlationId?: string;
  ts?: string;
  payload?: unknown;
};

export interface EngineHostStatus {
  ready: boolean;
  protocolVersion: number;
  restarts?: number;
  lastError?: string | null;
  hello?: {
    engineVersion?: string;
    schemaVersion?: number;
    keystore?: string;
    storageRecovered?: boolean;
    dataDir?: string;
  } | null;
}

export function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

/**
 * Development-only transport: talks to scripts/dev-bridge.mjs (loopback,
 * token-protected) so the UI can be exercised in a normal browser against the
 * real engine. Disabled in production builds.
 */
function bridgeConfig(): { url: string; token: string } | null {
  if (!import.meta.env.DEV || isTauri()) return null;
  const url = import.meta.env.VITE_RELAY_BRIDGE_URL as string | undefined;
  const token = import.meta.env.VITE_RELAY_BRIDGE_TOKEN as string | undefined;
  if (!url || !token) return null;
  return { url: url.replace(/\/$/, ""), token };
}

export function transportAvailable(): boolean {
  return isTauri() || bridgeConfig() !== null;
}

export async function engineRequest<T = unknown>(
  method: string,
  params?: unknown,
  timeoutMs = 30_000,
): Promise<T> {
  const bridge = bridgeConfig();
  try {
    if (bridge) {
      const res = await fetch(`${bridge.url}/rpc`, {
        method: "POST",
        headers: { "content-type": "application/json", authorization: `Bearer ${bridge.token}` },
        body: JSON.stringify({ method, params: params ?? null, timeoutMs }),
      });
      const body = (await res.json()) as { ok: boolean; result?: T; error?: unknown };
      if (!body.ok) throw EngineError.from(body.error);
      return body.result as T;
    }
    if (!isTauri()) {
      throw new EngineError(
        "engine_unavailable",
        "Relay is running outside the desktop app; no engine is available.",
      );
    }
    return await invoke<T>("engine_request", { method, params: params ?? null, timeoutMs });
  } catch (err) {
    throw EngineError.from(err);
  }
}

export async function engineStatus(): Promise<EngineHostStatus> {
  const bridge = bridgeConfig();
  if (bridge) {
    const res = await fetch(`${bridge.url}/status`, {
      headers: { authorization: `Bearer ${bridge.token}` },
    });
    return (await res.json()) as EngineHostStatus;
  }
  if (!isTauri()) return { ready: false, protocolVersion: 0, lastError: "No engine transport" };
  return invoke<EngineHostStatus>("engine_status");
}

export async function engineRestart(): Promise<EngineHostStatus> {
  const bridge = bridgeConfig();
  try {
    if (bridge) {
      const res = await fetch(`${bridge.url}/restart`, {
        method: "POST",
        headers: { authorization: `Bearer ${bridge.token}` },
      });
      return (await res.json()) as EngineHostStatus;
    }
    return await invoke<EngineHostStatus>("engine_restart");
  } catch (err) {
    throw EngineError.from(err);
  }
}

export async function onEngineEvent(handler: (ev: EngineEvent) => void): Promise<UnlistenFn> {
  const bridge = bridgeConfig();
  if (bridge) {
    const es = new EventSource(`${bridge.url}/events?token=${encodeURIComponent(bridge.token)}`);
    es.onmessage = (m) => {
      try {
        handler(JSON.parse(m.data) as EngineEvent);
      } catch {
        // ignore malformed frames
      }
    };
    return () => es.close();
  }
  if (!isTauri()) return () => {};
  return listen<EngineEvent>("engine-event", (e) => handler(e.payload));
}
