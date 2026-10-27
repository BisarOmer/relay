import { Plus, Send, X } from "lucide-react";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, Input, Segmented, Select, Textarea } from "@/components/ui/input";
import { Banner, CopyButton, EmptyState, KeyValue, PageHeader, SectionHeader } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { fmtTime } from "@/lib/format";
import { cn, decodeBytes, encodePayload, formatBytes, hexDump, isBinary, tryPrettyJson, validateSubject } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { confirm } from "@/stores/ui-store";
import type { InboundMessage } from "@/types/models";

type Mode = "publish" | "request";
type Format = "json" | "text";

interface HeaderRow {
  key: string;
  value: string;
}

interface HistoryEntry {
  at: string;
  mode: Mode;
  subject: string;
  ok: boolean;
  detail: string;
}

const HEADER_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

export function Publisher({ connectionId }: { connectionId?: string }) {
  const connections = useAppStore((s) => s.connections);
  const runtimeStatus = useAppStore((s) => s.runtimeStatus);
  const tz = useAppStore((s) => s.preferences.timezone);
  const appendLog = useAppStore((s) => s.appendLog);

  const [selectedId, setSelectedId] = useState(
    connectionId ?? connections.find((c) => runtimeStatus[c.id] === "connected" && !c.readOnly)?.id ?? "",
  );
  const [mode, setMode] = useState<Mode>("publish");
  const [subject, setSubject] = useState("");
  const [replyTo, setReplyTo] = useState("");
  const [format, setFormat] = useState<Format>("json");
  const [payload, setPayload] = useState('{\n  "hello": "relay"\n}');
  const [headers, setHeaders] = useState<HeaderRow[]>([]);
  const [timeoutMs, setTimeoutMs] = useState("5000");
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reply, setReply] = useState<{ msg: InboundMessage; ms: number } | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  const conn = connections.find((c) => c.id === selectedId);
  const connected = Boolean(selectedId) && runtimeStatus[selectedId] === "connected";

  const errors = useMemo(() => {
    const e: Record<string, string> = {};
    const subjErr = validateSubject(subject.trim(), false);
    if (subjErr) e.subject = subjErr;
    if (mode === "publish" && replyTo.trim()) {
      const r = validateSubject(replyTo.trim(), false);
      if (r) e.replyTo = r;
    }
    if (format === "json" && payload.trim()) {
      try {
        JSON.parse(payload);
      } catch (err) {
        e.payload = `Invalid JSON: ${(err as Error).message}`;
      }
    }
    headers.forEach((h, i) => {
      if (!h.key && !h.value) return;
      if (!HEADER_NAME.test(h.key)) e[`h${i}`] = "Invalid header name";
    });
    if (mode === "request") {
      const t = Number(timeoutMs);
      if (!Number.isInteger(t) || t < 100 || t > 60000) e.timeoutMs = "100 to 60000 ms";
    }
    return e;
  }, [subject, replyTo, format, payload, headers, mode, timeoutMs]);
  const valid = Object.keys(errors).length === 0;
  const payloadBytes = useMemo(() => new TextEncoder().encode(payload).length, [payload]);

  async function send() {
    setTouched(true);
    if (!valid || !conn || !connected || conn.readOnly) return;
    if (conn.environment === "production") {
      const ok = await confirm({
        title: mode === "publish" ? "Publish to production?" : "Send a request to production?",
        description: (
          <span>
            This sends a real message on <span className="font-mono">{subject.trim()}</span> through{" "}
            <span className="font-medium">{conn.name}</span>. Subscribers act on it immediately.
          </span>
        ),
        confirmLabel: mode === "publish" ? "Publish" : "Send request",
        tone: "danger",
      });
      if (!ok) return;
    }
    setBusy(true);
    setError(null);
    setReply(null);
    const hdrs: Record<string, string[]> = {};
    for (const h of headers) {
      if (!h.key) continue;
      (hdrs[h.key] ??= []).push(h.value);
    }
    const body = {
      connectionId: conn.id,
      subject: subject.trim(),
      replyTo: mode === "publish" && replyTo.trim() ? replyTo.trim() : undefined,
      headers: Object.keys(hdrs).length ? hdrs : undefined,
      payloadBase64: encodePayload(payload),
      contentType: format === "json" ? "application/json" : "text/plain",
    };
    const started = performance.now();
    const at = new Date().toISOString();
    try {
      if (mode === "publish") {
        await engineRequest("messages.publish", body);
        const ms = Math.round(performance.now() - started);
        appendLog(`Published ${payloadBytes} bytes to ${body.subject}`);
        setHistory((h) => [{ at, mode, subject: body.subject, ok: true, detail: `Flushed to server in ${ms} ms` }, ...h].slice(0, 20));
      } else {
        const t = Number(timeoutMs);
        const res = await engineRequest<InboundMessage>("messages.request", { ...body, timeoutMs: t }, t + 5000);
        const ms = Math.round(performance.now() - started);
        setReply({ msg: res, ms });
        appendLog(`Request on ${body.subject} answered in ${ms} ms`);
        setHistory((h) => [{ at, mode, subject: body.subject, ok: true, detail: `Reply in ${ms} ms` }, ...h].slice(0, 20));
      }
    } catch (e) {
      const msg = errorMessage(e);
      setError(msg);
      setHistory((h) => [{ at, mode, subject: body.subject, ok: false, detail: msg }, ...h].slice(0, 20));
    } finally {
      setBusy(false);
    }
  }

  const blocked = !conn ? "Choose a connection." : !connected ? `${conn.name} is not connected.` : conn.readOnly ? `${conn.name} is read-only. Publishing is disabled.` : null;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-auto">
      <PageHeader
        title="Publisher"
        description="Send a message or a request. Every send is explicit; nothing is retried automatically."
        meta={conn?.environment === "production" ? <Badge tone="production">production</Badge> : undefined}
      />
      <div className="grid min-h-0 flex-1 gap-5 p-5 lg:grid-cols-[minmax(0,1fr)_minmax(300px,380px)]">
        <form
          className="space-y-4"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-[1fr_auto]">
            <Field label="Connection" htmlFor="pub-conn">
              <Select id="pub-conn" value={selectedId} onChange={(e) => setSelectedId(e.target.value)}>
                <option value="">Select a connection…</option>
                {connections.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name} — {runtimeStatus[c.id] ?? c.lastStatus}
                    {c.readOnly ? " (read-only)" : ""}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Mode">
              <Segmented<Mode>
                ariaLabel="Mode"
                value={mode}
                onChange={setMode}
                options={[
                  { value: "publish", label: "Publish" },
                  { value: "request", label: "Request / reply" },
                ]}
              />
            </Field>
          </div>
          {blocked && conn && (
            <Banner tone={conn.readOnly ? "info" : "warning"} className="rounded-md">
              {blocked}
            </Banner>
          )}
          {conn?.environment === "production" && !blocked && (
            <Banner tone="production" className="rounded-md">
              Production connection. You will be asked to confirm before sending.
            </Banner>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Subject" required htmlFor="pub-subject" error={touched ? errors.subject : undefined} hint="Wildcards are not allowed when publishing.">
              <Input id="pub-subject" className="font-mono text-xs" placeholder="orders.created" value={subject} onChange={(e) => setSubject(e.target.value)} />
            </Field>
            {mode === "publish" ? (
              <Field label="Reply subject (optional)" htmlFor="pub-reply" error={touched ? errors.replyTo : undefined}>
                <Input id="pub-reply" className="font-mono text-xs" value={replyTo} onChange={(e) => setReplyTo(e.target.value)} />
              </Field>
            ) : (
              <Field label="Timeout (ms)" htmlFor="pub-timeout" error={errors.timeoutMs}>
                <Input id="pub-timeout" inputMode="numeric" className="tabular" value={timeoutMs} onChange={(e) => setTimeoutMs(e.target.value)} />
              </Field>
            )}
          </div>

          <div className="space-y-2">
            <SectionHeader
              title="Headers"
              actions={
                <Button type="button" size="xs" variant="ghost" onClick={() => setHeaders((h) => [...h, { key: "", value: "" }])}>
                  <Plus className="h-3 w-3" /> Add header
                </Button>
              }
            />
            {headers.length === 0 && <p className="text-[11px] text-fg-subtle">No headers.</p>}
            {headers.map((h, i) => (
              <div key={i} className="flex items-start gap-2">
                <div className="grid flex-1 gap-1">
                  <Input
                    aria-label={`Header ${i + 1} name`}
                    className={cn("font-mono text-xs", touched && errors[`h${i}`] && "border-danger")}
                    placeholder="Name"
                    value={h.key}
                    onChange={(e) => setHeaders((all) => all.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))}
                  />
                  {touched && errors[`h${i}`] && <span className="text-[11px] text-danger">{errors[`h${i}`]}</span>}
                </div>
                <Input
                  aria-label={`Header ${i + 1} value`}
                  className="flex-[2] font-mono text-xs"
                  placeholder="Value"
                  value={h.value}
                  onChange={(e) => setHeaders((all) => all.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
                />
                <Button type="button" size="icon-sm" variant="ghost" aria-label={`Remove header ${i + 1}`} onClick={() => setHeaders((all) => all.filter((_, j) => j !== i))}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
          </div>

          <Field
            label={
              <span className="flex items-center gap-2">
                Payload
                <span className="tabular font-normal text-fg-subtle">{formatBytes(payloadBytes)}</span>
              </span>
            }
            htmlFor="pub-payload"
            error={errors.payload}
          >
            <div className="flex items-center gap-2 pb-1">
              <Segmented<Format>
                size="xs"
                ariaLabel="Payload format"
                value={format}
                onChange={setFormat}
                options={[
                  { value: "json", label: "JSON" },
                  { value: "text", label: "Text" },
                ]}
              />
              {format === "json" && (
                <Button type="button" size="xs" variant="ghost" disabled={Boolean(errors.payload)} onClick={() => setPayload(tryPrettyJson(payload))}>
                  Format
                </Button>
              )}
            </div>
            <Textarea id="pub-payload" rows={12} spellCheck={false} className="font-mono text-xs" value={payload} onChange={(e) => setPayload(e.target.value)} />
          </Field>

          {error && (
            <div role="alert" className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
              {error}
            </div>
          )}
          <div className="flex items-center gap-2">
            <Button type="submit" disabled={Boolean(blocked) || busy} loading={busy}>
              <Send className="h-3.5 w-3.5" /> {mode === "publish" ? "Publish" : "Send request"}
            </Button>
            {touched && !valid && <span className="text-xs text-danger">Fix the highlighted fields.</span>}
          </div>
        </form>

        <div className="space-y-4">
          {mode === "request" && (
            <div className="rounded-lg border border-border bg-bg-panel">
              <div className="border-b border-border px-3 py-2">
                <SectionHeader title="Reply" />
              </div>
              {reply ? (
                <ReplyView reply={reply.msg} ms={reply.ms} tz={tz} />
              ) : error ? (
                <EmptyState compact title="No reply" description={error} />
              ) : (
                <EmptyState compact title="No reply yet" description="The response to your request appears here." />
              )}
            </div>
          )}
          <div className="rounded-lg border border-border bg-bg-panel">
            <div className="border-b border-border px-3 py-2">
              <SectionHeader title="This session" />
            </div>
            {history.length === 0 ? (
              <p className="p-3 text-xs text-fg-subtle">Nothing sent yet.</p>
            ) : (
              <ul className="divide-y divide-border">
                {history.map((h, i) => (
                  <li key={i} className="px-3 py-2 text-xs">
                    <div className="flex items-center gap-2">
                      <Badge tone={h.ok ? "success" : "danger"}>{h.ok ? "sent" : "failed"}</Badge>
                      <span className="truncate font-mono">{h.subject}</span>
                      <span className="tabular ml-auto text-fg-subtle">{fmtTime(h.at, tz)}</span>
                    </div>
                    <div className={cn("mt-0.5 break-words", h.ok ? "text-fg-subtle" : "text-danger")}>{h.detail}</div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function ReplyView({ reply, ms, tz }: { reply: InboundMessage; ms: number; tz: "local" | "utc" }) {
  const bytes = decodeBytes(reply.payloadBase64);
  const binary = isBinary(bytes);
  const text = binary ? hexDump(bytes) : tryPrettyJson(new TextDecoder().decode(bytes));
  const headerEntries = Object.entries(reply.headers ?? {});
  return (
    <div className="space-y-3 p-3">
      <KeyValue
        items={[
          ["Subject", <span className="font-mono">{reply.metadata.subject}</span>],
          ["Latency", `${ms} ms`],
          ["Received", fmtTime(reply.receivedAt, tz)],
          ["Size", formatBytes(reply.sizeBytes)],
          ...headerEntries.map(([k, v]) => [<span className="font-mono">{k}</span>, <span className="font-mono">{v.join(", ")}</span>] as [React.ReactNode, React.ReactNode]),
        ]}
      />
      <div className="relative">
        <CopyButton className="absolute right-1 top-1" text={text} label="Copy reply" />
        <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md border border-border bg-bg-inset p-2.5 font-mono text-[11.5px]">{text || "(empty)"}</pre>
      </div>
    </div>
  );
}
