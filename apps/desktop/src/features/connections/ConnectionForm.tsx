import { CheckCircle2, ShieldAlert, XCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox, Field, Input, Select, Textarea } from "@/components/ui/input";
import { Banner, Card, PageHeader, Spinner } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { useAppStore } from "@/stores/app-store";
import { toast } from "@/stores/ui-store";
import type { ConnectionProfile, EnvironmentKind, HealthStatus } from "@/types/models";

type AuthMethod = ConnectionProfile["auth"]["method"];

const AUTH_LABELS: Record<AuthMethod, string> = {
  none: "None",
  user_pass: "Username and password",
  token: "Token",
  nkey: "NKey seed",
  creds: "Credentials file (.creds)",
  tls_cert: "TLS client certificate",
};

const SECRET_AUTH: AuthMethod[] = ["user_pass", "token", "nkey"];
const SCHEMES = ["nats", "tls", "ws", "wss"];

export interface FormState {
  name: string;
  servers: string;
  environment: EnvironmentKind;
  environmentName: string;
  authMethod: AuthMethod;
  username: string;
  secret: string;
  clearSecret: boolean;
  credsPath: string;
  tlsEnabled: boolean;
  caCertPath: string;
  clientCertPath: string;
  clientKeyPath: string;
  serverName: string;
  timeoutMs: string;
  reconnectEnabled: boolean;
  initialBackoffMs: string;
  maxBackoffMs: string;
  maxAttempts: string;
  readOnly: boolean;
}

export const EMPTY_FORM: FormState = {
  name: "",
  servers: "nats://127.0.0.1:4222",
  environment: "local",
  environmentName: "",
  authMethod: "none",
  username: "",
  secret: "",
  clearSecret: false,
  credsPath: "",
  tlsEnabled: false,
  caCertPath: "",
  clientCertPath: "",
  clientKeyPath: "",
  serverName: "",
  timeoutMs: "10000",
  reconnectEnabled: true,
  initialBackoffMs: "500",
  maxBackoffMs: "10000",
  maxAttempts: "0",
  readOnly: false,
};

function fromProfile(p: ConnectionProfile): FormState {
  return {
    name: p.name,
    servers: p.servers.join("\n"),
    environment: p.environment,
    environmentName: p.environmentName ?? "",
    authMethod: p.auth.method,
    username: p.auth.username ?? "",
    secret: "",
    clearSecret: false,
    credsPath: p.auth.credsPath ?? "",
    tlsEnabled: p.tls.enabled,
    caCertPath: p.tls.caCertPath ?? "",
    clientCertPath: p.tls.clientCertPath ?? "",
    clientKeyPath: p.tls.clientKeyPath ?? "",
    serverName: p.tls.serverName ?? "",
    timeoutMs: String(p.timeoutMs),
    reconnectEnabled: p.reconnect.enabled,
    initialBackoffMs: String(p.reconnect.initialBackoffMs),
    maxBackoffMs: String(p.reconnect.maxBackoffMs),
    maxAttempts: String(p.reconnect.maxAttempts),
    readOnly: p.readOnly,
  };
}

function parseServers(raw: string) {
  return raw
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function intIn(v: string, min: number, max: number) {
  if (!/^\d+$/.test(v.trim())) return null;
  const n = Number(v);
  return n >= min && n <= max ? n : null;
}

export function validateForm(f: FormState, hasStoredSecret: boolean): Partial<Record<keyof FormState, string>> {
  const e: Partial<Record<keyof FormState, string>> = {};
  if (!f.name.trim()) e.name = "Name is required";
  else if (f.name.trim().length > 120) e.name = "Name must be 120 characters or fewer";
  const servers = parseServers(f.servers);
  if (servers.length === 0) e.servers = "At least one server URL is required";
  else if (servers.length > 32) e.servers = "At most 32 servers";
  else {
    for (const s of servers) {
      const withScheme = s.includes("://") ? s : `nats://${s}`;
      let u: URL;
      try {
        u = new URL(withScheme);
      } catch {
        e.servers = `Not a valid URL: ${s}`;
        break;
      }
      const scheme = u.protocol.replace(":", "");
      if (!SCHEMES.includes(scheme)) {
        e.servers = `Unsupported scheme "${scheme}" in ${s} (use nats, tls, ws or wss)`;
        break;
      }
      if (u.username || u.password) {
        e.servers = "Do not put credentials in the URL. Use the Authentication section instead.";
        break;
      }
      if (!u.hostname) {
        e.servers = `Missing host in ${s}`;
        break;
      }
    }
  }
  if (f.environment === "custom" && !f.environmentName.trim()) e.environmentName = "Name the custom environment";
  if (intIn(f.timeoutMs, 500, 120000) === null) e.timeoutMs = "Between 500 and 120000 ms";
  if (f.reconnectEnabled) {
    const init = intIn(f.initialBackoffMs, 50, 60000);
    const max = intIn(f.maxBackoffMs, 50, 300000);
    if (init === null) e.initialBackoffMs = "Between 50 and 60000 ms";
    if (max === null) e.maxBackoffMs = "Between 50 and 300000 ms";
    else if (init !== null && max < init) e.maxBackoffMs = "Must be at least the initial delay";
    if (intIn(f.maxAttempts, 0, 100000) === null) e.maxAttempts = "0 (unlimited) to 100000";
  }
  if (f.authMethod === "user_pass" && !f.username.trim()) e.username = "Username is required";
  if (SECRET_AUTH.includes(f.authMethod) && !f.secret && (!hasStoredSecret || f.clearSecret)) {
    e.secret = f.authMethod === "user_pass" ? "Password is required" : f.authMethod === "token" ? "Token is required" : "Seed is required";
  }
  if (f.authMethod === "nkey" && f.secret && !/^SU[A-Z2-7]{50,}$/.test(f.secret.trim())) {
    e.secret = "Expected a user seed starting with SU";
  }
  if (f.authMethod === "creds" && !f.credsPath.trim()) e.credsPath = "Path to a .creds file is required";
  if (f.authMethod === "tls_cert") {
    if (!f.clientCertPath.trim()) e.clientCertPath = "Client certificate is required";
    if (!f.clientKeyPath.trim()) e.clientKeyPath = "Client key is required";
  }
  if (Boolean(f.clientCertPath.trim()) !== Boolean(f.clientKeyPath.trim()) && f.authMethod !== "tls_cert") {
    e.clientKeyPath = "Provide both a client certificate and key, or neither";
  }
  return e;
}

function toProfile(f: FormState, base?: ConnectionProfile) {
  const now = new Date().toISOString();
  const tlsNeeded = f.tlsEnabled || f.authMethod === "tls_cert";
  return {
    id: base?.id ?? "",
    name: f.name.trim(),
    brokerType: "nats" as const,
    environment: f.environment,
    environmentName: f.environment === "custom" ? f.environmentName.trim() : undefined,
    servers: parseServers(f.servers),
    auth: {
      method: f.authMethod,
      username: f.authMethod === "user_pass" ? f.username.trim() : undefined,
      credsPath: f.authMethod === "creds" ? f.credsPath.trim() : undefined,
    },
    tls: {
      enabled: tlsNeeded,
      caCertPath: tlsNeeded ? f.caCertPath.trim() || undefined : undefined,
      clientCertPath: tlsNeeded ? f.clientCertPath.trim() || undefined : undefined,
      clientKeyPath: tlsNeeded ? f.clientKeyPath.trim() || undefined : undefined,
      serverName: tlsNeeded ? f.serverName.trim() || undefined : undefined,
      insecureSkipVerify: false,
    },
    timeoutMs: Number(f.timeoutMs),
    reconnect: {
      enabled: f.reconnectEnabled,
      initialBackoffMs: Number(f.initialBackoffMs),
      maxBackoffMs: Number(f.maxBackoffMs),
      maxAttempts: Number(f.maxAttempts),
    },
    tags: base?.tags,
    metadata: base?.metadata,
    readOnly: f.readOnly,
    createdAt: base?.createdAt ?? now,
    updatedAt: now,
    lastStatus: base?.lastStatus ?? ("disconnected" as const),
  };
}

export function ConnectionForm({ tabId, connectionId }: { tabId: string; connectionId?: string }) {
  const connections = useAppStore((s) => s.connections);
  const refreshConnections = useAppStore((s) => s.refreshConnections);
  const appendLog = useAppStore((s) => s.appendLog);
  const openTab = useAppStore((s) => s.openTab);
  const closeTab = useAppStore((s) => s.closeTab);
  const connect = useAppStore((s) => s.connect);
  const engineReady = useAppStore((s) => s.engineState === "ready");

  const existing = connectionId ? connections.find((c) => c.id === connectionId) : undefined;
  const hasStoredSecret = Boolean(existing?.auth.secretRef);
  const [form, setForm] = useState<FormState>(() => (existing ? fromProfile(existing) : EMPTY_FORM));
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState<"save" | "connect" | "test" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string; details?: Record<string, string> } | null>(null);

  useEffect(() => {
    if (existing) setForm(fromProfile(existing));
    // Only reset when switching to a different connection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [existing?.id]);

  const errors = useMemo(() => validateForm(form, hasStoredSecret), [form, hasStoredSecret]);
  const valid = Object.keys(errors).length === 0;
  const show = (k: keyof FormState) => (touched ? errors[k] : undefined);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => {
    setForm((f) => ({ ...f, [k]: v }));
    setTestResult(null);
  };

  const authChanged = existing && existing.auth.method !== form.authMethod;
  const needsSecret = SECRET_AUTH.includes(form.authMethod);
  const secretLabel = form.authMethod === "user_pass" ? "Password" : form.authMethod === "token" ? "Token" : "NKey seed";

  async function test() {
    setTouched(true);
    if (!valid) return;
    setBusy("test");
    setTestResult(null);
    try {
      const res = await engineRequest<HealthStatus>(
        "connections.test",
        { id: existing && !authChanged ? existing.id : undefined, profile: toProfile(form, existing), secret: form.secret || undefined },
        Number(form.timeoutMs) + 10_000,
      );
      setTestResult({ ok: res.healthy, message: res.message ?? (res.healthy ? "Connected" : "Failed"), details: res.details });
    } catch (e) {
      setTestResult({ ok: false, message: errorMessage(e) });
    } finally {
      setBusy(null);
    }
  }

  async function save(connectAfter: boolean) {
    setTouched(true);
    if (!valid) return;
    setBusy(connectAfter ? "connect" : "save");
    setError(null);
    try {
      const payload = {
        profile: toProfile(form, existing),
        secret: needsSecret && form.secret ? form.secret : undefined,
        clearSecret: existing ? (!needsSecret && hasStoredSecret) || form.clearSecret : undefined,
      };
      const saved = await engineRequest<ConnectionProfile>(existing ? "connections.update" : "connections.create", payload);
      appendLog(`${existing ? "Updated" : "Saved"} connection ${saved.name}`);
      toast.success(existing ? "Connection updated" : "Connection saved", saved.name);
      await refreshConnections();
      closeTab(tabId);
      openTab({ id: `conn-${saved.id}`, kind: "connection", title: saved.name, connectionId: saved.id });
      if (connectAfter) await connect(saved.id);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  if (connectionId && !existing) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading connection…" />
      </div>
    );
  }

  return (
    <div className="h-full overflow-auto">
      <PageHeader
        title={existing ? `Edit ${existing.name}` : "New NATS connection"}
        description="Use a client URL (port 4222 by default). The HTTP monitoring port (8222) is configured in Insights."
      />
      <form
        className="mx-auto max-w-2xl space-y-5 p-6"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void save(false);
        }}
      >
        {existing && existing.lastStatus !== "disconnected" && existing.lastStatus !== "error" && (
          <Banner tone="info" className="rounded-md">Changes apply the next time you connect. The current session keeps its settings.</Banner>
        )}

        <Card className="space-y-4 p-4">
          <Field label="Name" required htmlFor="cf-name" error={show("name")}>
            <Input id="cf-name" value={form.name} autoFocus={!existing} placeholder="Orders cluster" onChange={(e) => set("name", e.target.value)} />
          </Field>
          <Field
            label="Server URLs"
            required
            htmlFor="cf-servers"
            error={show("servers")}
            hint="One per line. nats://, tls://, ws:// or wss://. A missing scheme defaults to nats://."
          >
            <Textarea id="cf-servers" rows={3} className="font-mono text-xs" value={form.servers} onChange={(e) => set("servers", e.target.value)} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Environment" htmlFor="cf-env" hint={form.environment === "production" ? "Publishing will ask for confirmation." : undefined}>
              <Select id="cf-env" value={form.environment} onChange={(e) => set("environment", e.target.value as EnvironmentKind)}>
                <option value="local">Local</option>
                <option value="development">Development</option>
                <option value="staging">Staging</option>
                <option value="production">Production</option>
                <option value="custom">Custom…</option>
              </Select>
            </Field>
            {form.environment === "custom" && (
              <Field label="Environment name" required htmlFor="cf-envname" error={show("environmentName")}>
                <Input id="cf-envname" value={form.environmentName} onChange={(e) => set("environmentName", e.target.value)} />
              </Field>
            )}
          </div>
          <Checkbox
            checked={form.readOnly}
            onChange={(v) => set("readOnly", v)}
            label="Read-only"
            description="Disables publish and request/reply for this connection. Subscribing and browsing still work."
          />
        </Card>

        <Card className="space-y-4 p-4">
          <h3 className="text-[13px] font-semibold">Authentication</h3>
          <Field label="Method" htmlFor="cf-auth">
            <Select id="cf-auth" value={form.authMethod} onChange={(e) => set("authMethod", e.target.value as AuthMethod)}>
              {(Object.keys(AUTH_LABELS) as AuthMethod[]).map((m) => (
                <option key={m} value={m}>
                  {AUTH_LABELS[m]}
                </option>
              ))}
            </Select>
          </Field>
          {form.authMethod === "user_pass" && (
            <Field label="Username" required htmlFor="cf-user" error={show("username")}>
              <Input id="cf-user" autoComplete="off" value={form.username} onChange={(e) => set("username", e.target.value)} />
            </Field>
          )}
          {needsSecret && (
            <Field
              label={secretLabel}
              required={!hasStoredSecret || Boolean(authChanged)}
              htmlFor="cf-secret"
              error={show("secret")}
              hint={
                hasStoredSecret && !authChanged
                  ? "A secret is stored. Leave blank to keep it."
                  : "Stored in the engine's secret store, never in the database or logs."
              }
            >
              <Input
                id="cf-secret"
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                className="font-mono"
                placeholder={hasStoredSecret && !authChanged ? "••••••••" : undefined}
                value={form.secret}
                onChange={(e) => set("secret", e.target.value)}
              />
            </Field>
          )}
          {needsSecret && hasStoredSecret && !authChanged && (
            <Checkbox checked={form.clearSecret} onChange={(v) => set("clearSecret", v)} label="Remove the stored secret" />
          )}
          {form.authMethod === "creds" && (
            <Field label="Credentials file path" required htmlFor="cf-creds" error={show("credsPath")} hint="Absolute path to a .creds file (max 1 MiB).">
              <Input id="cf-creds" className="font-mono text-xs" placeholder="/Users/me/.nats/user.creds" value={form.credsPath} onChange={(e) => set("credsPath", e.target.value)} />
            </Field>
          )}
        </Card>

        <Card className="space-y-4 p-4">
          <h3 className="text-[13px] font-semibold">TLS</h3>
          {form.authMethod === "tls_cert" ? (
            <p className="text-xs text-fg-muted">TLS is required for client certificate authentication.</p>
          ) : (
            <Checkbox
              checked={form.tlsEnabled}
              onChange={(v) => set("tlsEnabled", v)}
              label="Require TLS"
              description="Server certificates are always verified. Skipping verification is not supported."
            />
          )}
          {(form.tlsEnabled || form.authMethod === "tls_cert") && (
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="CA certificate (optional)" htmlFor="cf-ca" hint="PEM file. Uses system roots when empty.">
                <Input id="cf-ca" className="font-mono text-xs" value={form.caCertPath} onChange={(e) => set("caCertPath", e.target.value)} />
              </Field>
              <Field label="Server name override" htmlFor="cf-sni" hint="Hostname to verify instead of the URL host.">
                <Input id="cf-sni" className="font-mono text-xs" value={form.serverName} onChange={(e) => set("serverName", e.target.value)} />
              </Field>
              <Field label="Client certificate" required={form.authMethod === "tls_cert"} htmlFor="cf-cert" error={show("clientCertPath")}>
                <Input id="cf-cert" className="font-mono text-xs" value={form.clientCertPath} onChange={(e) => set("clientCertPath", e.target.value)} />
              </Field>
              <Field label="Client key" required={form.authMethod === "tls_cert"} htmlFor="cf-key" error={show("clientKeyPath")}>
                <Input id="cf-key" className="font-mono text-xs" value={form.clientKeyPath} onChange={(e) => set("clientKeyPath", e.target.value)} />
              </Field>
            </div>
          )}
        </Card>

        <Card className="space-y-4 p-4">
          <h3 className="text-[13px] font-semibold">Timeouts and reconnect</h3>
          <Field label="Connect timeout (ms)" htmlFor="cf-timeout" error={show("timeoutMs")} className="max-w-[220px]">
            <Input id="cf-timeout" inputMode="numeric" className="tabular" value={form.timeoutMs} onChange={(e) => set("timeoutMs", e.target.value)} />
          </Field>
          <Checkbox
            checked={form.reconnectEnabled}
            onChange={(v) => set("reconnectEnabled", v)}
            label="Reconnect automatically"
            description="Exponential backoff with jitter between the initial and maximum delay."
          />
          {form.reconnectEnabled && (
            <div className="grid gap-4 sm:grid-cols-3">
              <Field label="Initial delay (ms)" htmlFor="cf-ib" error={show("initialBackoffMs")}>
                <Input id="cf-ib" inputMode="numeric" className="tabular" value={form.initialBackoffMs} onChange={(e) => set("initialBackoffMs", e.target.value)} />
              </Field>
              <Field label="Maximum delay (ms)" htmlFor="cf-mb" error={show("maxBackoffMs")}>
                <Input id="cf-mb" inputMode="numeric" className="tabular" value={form.maxBackoffMs} onChange={(e) => set("maxBackoffMs", e.target.value)} />
              </Field>
              <Field label="Max attempts" htmlFor="cf-ma" error={show("maxAttempts")} hint="0 means unlimited">
                <Input id="cf-ma" inputMode="numeric" className="tabular" value={form.maxAttempts} onChange={(e) => set("maxAttempts", e.target.value)} />
              </Field>
            </div>
          )}
        </Card>

        {testResult && (
          <div
            role="status"
            className={
              testResult.ok
                ? "flex items-start gap-2 rounded-md border border-success/30 bg-success/10 px-3 py-2 text-xs text-success"
                : "flex items-start gap-2 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger"
            }
          >
            {testResult.ok ? <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" /> : <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
            <div className="min-w-0">
              <div className="font-medium">{testResult.ok ? "Connection succeeded" : "Connection failed"}</div>
              <div className="break-words opacity-90">{testResult.message}</div>
              {testResult.details && (
                <div className="mt-1 font-mono text-[11px] text-fg-muted">
                  {Object.entries(testResult.details)
                    .filter(([, v]) => v)
                    .map(([k, v]) => `${k}: ${v}`)
                    .join(" · ")}
                </div>
              )}
            </div>
          </div>
        )}
        {error && (
          <div role="alert" className="flex items-start gap-2 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
            <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            <span className="break-words">{error}</span>
          </div>
        )}
        {touched && !valid && <p className="text-xs text-danger">Fix the highlighted fields to continue.</p>}

        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-4">
          <Button type="submit" disabled={!engineReady || busy !== null} loading={busy === "save"}>
            {existing ? "Save changes" : "Save"}
          </Button>
          <Button type="button" variant="secondary" disabled={!engineReady || busy !== null} loading={busy === "connect"} onClick={() => void save(true)}>
            Save and connect
          </Button>
          <Button type="button" variant="outline" disabled={!engineReady || busy !== null} loading={busy === "test"} onClick={() => void test()}>
            Test connection
          </Button>
          <Button type="button" variant="ghost" className="ml-auto" onClick={() => closeTab(tabId)}>
            Cancel
          </Button>
        </div>
      </form>
    </div>
  );
}
