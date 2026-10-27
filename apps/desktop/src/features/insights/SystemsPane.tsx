import { Pencil, Plus, Radar, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge, healthTone } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Checkbox, Field, Input } from "@/components/ui/input";
import { Banner, EmptyState, PageHeader } from "@/components/ui/misc";
import { errorMessage } from "@/lib/engine";
import { fmtDurationMs, fmtNumber, fmtRelative } from "@/lib/format";
import { confirm, toast } from "@/stores/ui-store";
import { insightsApi, type SystemInput } from "./api";
import type { InsightsSystem } from "./types";

export interface SystemForm {
  name: string;
  url: string;
  intervalSec: string;
  timeoutMs: string;
  retention: string;
  enabled: boolean;
  demo: boolean;
}

export const NEW_SYSTEM_FORM: SystemForm = { name: "", url: "http://127.0.0.1:8222", intervalSec: "5", timeoutMs: "3000", retention: "2880", enabled: true, demo: false };

export function validateSystemForm(f: SystemForm): Partial<Record<keyof SystemForm, string>> {
  const e: Partial<Record<keyof SystemForm, string>> = {};
  if (!f.name.trim()) e.name = "Name is required";
  else if (f.name.trim().length > 120) e.name = "At most 120 characters";
  if (!f.demo) {
    try {
      const u = new URL(f.url.trim());
      if (u.protocol !== "http:" && u.protocol !== "https:") e.url = "Use http:// or https://";
      else if (u.username || u.password) e.url = "Credentials in the URL are not allowed";
      else if (!u.hostname) e.url = "Missing host";
    } catch {
      e.url = "Enter a URL such as http://nats-host:8222";
    }
  }
  const interval = Number(f.intervalSec);
  if (!Number.isFinite(interval) || interval < 1 || interval > 3600) e.intervalSec = "1 to 3600 seconds";
  const timeout = Number(f.timeoutMs);
  if (!Number.isInteger(timeout) || timeout < 250 || timeout > 60000) e.timeoutMs = "250 to 60000 ms";
  else if (!e.intervalSec && timeout > interval * 1000) e.timeoutMs = "Must not exceed the collection interval";
  const retention = Number(f.retention);
  if (!Number.isInteger(retention) || retention < 10 || retention > 100000) e.retention = "10 to 100000 snapshots";
  return e;
}

export function SystemsPane({
  systems,
  onChanged,
  onDeleted,
}: {
  systems: InsightsSystem[];
  onChanged: (selectId?: string) => void;
  onDeleted: (id: string) => void;
}) {
  const [editing, setEditing] = useState<InsightsSystem | "new" | null>(null);

  async function toggle(s: InsightsSystem) {
    try {
      await insightsApi.upsertSystem({ ...pick(s), enabled: !s.enabled });
      onChanged();
    } catch (e) {
      toast.error("Could not update system", errorMessage(e));
    }
  }

  async function remove(s: InsightsSystem) {
    const ok = await confirm({
      title: `Remove ${s.name}?`,
      description: "All snapshots, metrics and findings collected for this system are deleted. The monitored server is not affected.",
      confirmLabel: "Remove",
      tone: "danger",
    });
    if (!ok) return;
    try {
      await insightsApi.deleteSystem(s.id);
      toast.success("System removed", s.name);
      onDeleted(s.id);
    } catch (e) {
      toast.error("Could not remove system", errorMessage(e));
    }
  }

  return (
    <div className="h-full overflow-auto">
      <PageHeader
        title="Monitored systems"
        description="Insights polls a NATS server's HTTP monitoring endpoint (varz, connz, routez, gatewayz, leafz, jsz…). It never connects as a client and never changes server state."
        actions={
          <Button size="sm" onClick={() => setEditing("new")}>
            <Plus className="h-3.5 w-3.5" /> Add system
          </Button>
        }
      />
      {systems.length === 0 ? (
        <EmptyState
          icon={Radar}
          title="No monitored systems"
          description="Add the monitoring URL of a NATS server (started with -m 8222 or http_port) to collect health, topology and diagnostics."
          action={
            <Button size="sm" onClick={() => setEditing("new")}>
              <Plus className="h-3.5 w-3.5" /> Add system
            </Button>
          }
        />
      ) : (
        <div className="p-5">
          <div className="overflow-hidden rounded-lg border border-border">
            <table className="w-full text-xs">
              <thead className="bg-bg-panel text-left text-[11px] text-fg-subtle">
                <tr>
                  <th className="px-4 py-2 font-medium">Name</th>
                  <th className="px-4 py-2 font-medium">Endpoint</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 font-medium">Interval</th>
                  <th className="px-4 py-2 font-medium">Retention</th>
                  <th className="px-4 py-2 font-medium">Last success</th>
                  <th className="px-4 py-2 font-medium">Collection</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody>
                {systems.map((s) => (
                  <tr key={s.id} className="border-t border-border">
                    <td className="px-4 py-2 font-medium">
                      {s.name} {s.demo && <Badge tone="warning">simulated</Badge>}
                    </td>
                    <td className="px-4 py-2 font-mono text-fg-muted">{s.demo ? "—" : s.monitoringBaseUrl}</td>
                    <td className="px-4 py-2">
                      <Badge tone={healthTone(s.lastStatus)} dot title={s.lastError}>
                        {s.lastStatus}
                      </Badge>
                    </td>
                    <td className="px-4 py-2">{fmtDurationMs(s.collectIntervalMs)}</td>
                    <td className="tabular px-4 py-2">{fmtNumber(s.retentionEpochs)} snapshots</td>
                    <td className="px-4 py-2 text-fg-muted">{fmtRelative(s.lastSuccessAt)}</td>
                    <td className="px-4 py-2">
                      <button type="button" role="switch" aria-checked={s.enabled} onClick={() => void toggle(s)} className="text-[11px] hover:underline">
                        {s.enabled ? (s.running ? <span className="text-success">running</span> : "enabled") : <span className="text-fg-subtle">paused</span>}
                      </button>
                    </td>
                    <td className="px-4 py-2 text-right">
                      <div className="inline-flex gap-0.5">
                        <Button size="icon-xs" variant="ghost" aria-label={`Edit ${s.name}`} onClick={() => setEditing(s)}>
                          <Pencil className="h-3 w-3" />
                        </Button>
                        <Button size="icon-xs" variant="ghost" aria-label={`Remove ${s.name}`} onClick={() => void remove(s)}>
                          <Trash2 className="h-3 w-3 text-danger" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      {editing && (
        <SystemDialog
          system={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(id, isNew) => {
            setEditing(null);
            onChanged(isNew ? id : undefined);
          }}
        />
      )}
    </div>
  );
}

function pick(s: InsightsSystem): SystemInput {
  return {
    id: s.id,
    name: s.name,
    monitoringBaseUrl: s.monitoringBaseUrl,
    collectIntervalMs: s.collectIntervalMs,
    requestTimeoutMs: s.requestTimeoutMs,
    retentionEpochs: s.retentionEpochs,
    enabled: s.enabled,
    demo: s.demo,
  };
}

function SystemDialog({ system, onClose, onSaved }: { system: InsightsSystem | null; onClose: () => void; onSaved: (id: string, isNew: boolean) => void }) {
  const [form, setForm] = useState<SystemForm>(() =>
    system
      ? {
          name: system.name,
          url: system.monitoringBaseUrl,
          intervalSec: String(system.collectIntervalMs / 1000),
          timeoutMs: String(system.requestTimeoutMs),
          retention: String(system.retentionEpochs),
          enabled: system.enabled,
          demo: system.demo,
        }
      : NEW_SYSTEM_FORM,
  );
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errors = useMemo(() => validateSystemForm(form), [form]);
  const valid = Object.keys(errors).length === 0;
  const set = <K extends keyof SystemForm>(k: K, v: SystemForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const show = (k: keyof SystemForm) => (touched ? errors[k] : undefined);

  async function save() {
    setTouched(true);
    if (!valid) return;
    setBusy(true);
    setError(null);
    try {
      const saved = await insightsApi.upsertSystem({
        id: system?.id,
        name: form.name.trim(),
        monitoringBaseUrl: form.demo ? "" : form.url.trim(),
        collectIntervalMs: Math.round(Number(form.intervalSec) * 1000),
        requestTimeoutMs: Number(form.timeoutMs),
        retentionEpochs: Number(form.retention),
        enabled: form.enabled,
        demo: form.demo,
      });
      toast.success(system ? "System updated" : "System added", saved.name);
      onSaved(saved.id, !system);
      // Enabled systems start collecting on their own as soon as they are saved.
      if (!system && !saved.enabled) void insightsApi.collect(saved.id).catch(() => undefined);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const retentionSpan = Number(form.retention) * Number(form.intervalSec) * 1000;

  return (
    <Dialog
      open
      onClose={onClose}
      title={system ? `Edit ${system.name}` : "Add monitored system"}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={busy} onClick={() => void save()}>
            {system ? "Save" : "Add and collect"}
          </Button>
        </>
      }
    >
      <form
        className="space-y-4"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <Field label="Name" required htmlFor="sys-name" error={show("name")}>
          <Input id="sys-name" autoFocus value={form.name} placeholder="Production cluster east" onChange={(e) => set("name", e.target.value)} />
        </Field>
        {!form.demo && (
          <Field label="Monitoring URL" required htmlFor="sys-url" error={show("url")} hint="The HTTP monitoring port, not the client port. Redirects are not followed.">
            <Input id="sys-url" className="font-mono text-xs" value={form.url} onChange={(e) => set("url", e.target.value)} />
          </Field>
        )}
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="Interval (s)" htmlFor="sys-int" error={show("intervalSec")}>
            <Input id="sys-int" inputMode="decimal" className="tabular" value={form.intervalSec} onChange={(e) => set("intervalSec", e.target.value)} />
          </Field>
          <Field label="Request timeout (ms)" htmlFor="sys-to" error={show("timeoutMs")}>
            <Input id="sys-to" inputMode="numeric" className="tabular" value={form.timeoutMs} onChange={(e) => set("timeoutMs", e.target.value)} />
          </Field>
          <Field
            label="Keep snapshots"
            htmlFor="sys-ret"
            error={show("retention")}
            hint={!errors.retention && !errors.intervalSec ? `≈ ${fmtDurationMs(retentionSpan)} of history` : undefined}
          >
            <Input id="sys-ret" inputMode="numeric" className="tabular" value={form.retention} onChange={(e) => set("retention", e.target.value)} />
          </Field>
        </div>
        <Checkbox checked={form.enabled} onChange={(v) => set("enabled", v)} label="Collect automatically" description="When off, snapshots are only taken with “Collect now”." />
        <Checkbox
          checked={form.demo}
          onChange={(v) => set("demo", v)}
          label="Simulated demo data"
          description="Generates synthetic snapshots without any network access. Everything it shows is labeled as simulated."
        />
        {form.demo && (
          <Banner tone="warning" className="rounded-md">
            Demo systems never contact a server. Use them to explore Insights, not to monitor anything.
          </Banner>
        )}
        {error && (
          <p role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
            {error}
          </p>
        )}
        <button type="submit" className="hidden" aria-hidden tabIndex={-1} />
      </form>
    </Dialog>
  );
}
