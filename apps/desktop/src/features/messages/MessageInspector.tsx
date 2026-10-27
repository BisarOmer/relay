import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDownToLine, Pause, Play, Radio, Square, Trash2, Users } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input, Select } from "@/components/ui/input";
import { Banner, CopyButton, EmptyState, KeyValue, Tabs } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { fmtDateTime, fmtTime } from "@/lib/format";
import { cn, decodeBytes, formatBytes, hexDump, isBinary, tryPrettyJson, validateSubject } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import { toast } from "@/stores/ui-store";
import type { InboundMessage, SubscriptionStatus } from "@/types/models";

type DetailTab = "payload" | "headers" | "metadata";
type PayloadView = "auto" | "text" | "hex" | "base64";

const textDecoder = new TextDecoder();

function preview(m: InboundMessage) {
  const bytes = decodeBytes(m.payloadBase64);
  if (bytes.length === 0) return { text: "(empty)", binary: false };
  if (isBinary(bytes)) return { text: `binary · ${bytes.length} bytes`, binary: true };
  return { text: textDecoder.decode(bytes.subarray(0, 200)).replace(/\s+/g, " "), binary: false };
}

export function MessageInspector({ connectionId, initialSubject = ">" }: { connectionId: string; initialSubject?: string }) {
  const conn = useAppStore((s) => s.connections.find((c) => c.id === connectionId));
  const status = useAppStore((s) => s.runtimeStatus[connectionId]);
  const allMessages = useAppStore((s) => s.messages);
  const allSubs = useAppStore((s) => s.subscriptions);
  const selectedMessageId = useAppStore((s) => s.selectedMessageId);
  const followLatest = useAppStore((s) => s.followLatest);
  const tz = useAppStore((s) => s.preferences.timezone);
  const { setSelectedMessage, setFollowLatest, clearMessages, appendLog, connect } = useAppStore.getState();

  const subs = useMemo(() => allSubs.filter((s) => s.connectionId === connectionId), [allSubs, connectionId]);
  const [subject, setSubject] = useState(initialSubject);
  const [queueMode, setQueueMode] = useState(false);
  const [queueGroup, setQueueGroup] = useState("");
  const [subError, setSubError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const [subFilter, setSubFilter] = useState<string>("all");
  const [detailTab, setDetailTab] = useState<DetailTab>("payload");
  const [payloadView, setPayloadView] = useState<PayloadView>("auto");
  const parentRef = useRef<HTMLDivElement>(null);
  const connected = status === "connected";

  const subjectError = subject ? validateSubject(subject.trim(), true) : null;

  const messages = useMemo(() => {
    let list = allMessages.filter((m) => m.connectionId === connectionId);
    if (subFilter !== "all") list = list.filter((m) => m.subscriptionId === subFilter);
    const q = filter.trim().toLowerCase();
    if (!q) return list;
    return list.filter((m) => {
      if ((m.metadata.subject ?? "").toLowerCase().includes(q)) return true;
      if (m.headers && JSON.stringify(m.headers).toLowerCase().includes(q)) return true;
      const bytes = decodeBytes(m.payloadBase64);
      return !isBinary(bytes) && textDecoder.decode(bytes).toLowerCase().includes(q);
    });
  }, [allMessages, connectionId, subFilter, filter]);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 30,
    overscan: 20,
  });

  useEffect(() => {
    if (followLatest && messages.length > 0) virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
  }, [followLatest, messages.length, virtualizer]);

  const selected = messages.find((m) => m.id === selectedMessageId) ?? (followLatest ? messages.at(-1) : undefined);

  async function subscribe() {
    const s = subject.trim();
    const err = validateSubject(s, true) ?? (queueMode && !queueGroup.trim() ? "Queue group name is required" : null);
    if (err) {
      setSubError(err);
      return;
    }
    if (subs.some((x) => x.subject === s && x.mode === (queueMode ? "queue" : "observe"))) {
      setSubError(`Already subscribed to ${s}`);
      return;
    }
    setSubError(null);
    setBusy(true);
    try {
      await engineRequest<SubscriptionStatus>("messages.subscribe", {
        connectionId,
        subject: s,
        mode: queueMode ? "queue" : "observe",
        queueGroup: queueMode ? queueGroup.trim() : undefined,
      });
      appendLog(`Subscribed to ${s}${queueMode ? ` (queue ${queueGroup})` : ""}`);
      setFollowLatest(true);
    } catch (e) {
      setSubError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function subAction(sub: SubscriptionStatus, action: "pause" | "resume" | "unsubscribe") {
    try {
      await engineRequest(`messages.${action}`, { id: sub.id });
      if (action === "unsubscribe") {
        appendLog(`Unsubscribed from ${sub.subject}`);
        if (subFilter === sub.id) setSubFilter("all");
      }
    } catch (e) {
      toast.error(`Could not ${action} ${sub.subject}`, errorMessage(e));
    }
  }

  if (!conn) return <EmptyState title="Connection not found" />;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <form
        className="flex flex-wrap items-start gap-2 border-b border-border px-3 py-2"
        onSubmit={(e) => {
          e.preventDefault();
          void subscribe();
        }}
      >
        <div className="grid gap-1">
          <Input
            aria-label="Subject to subscribe to"
            className={cn("w-72 font-mono text-xs", (subjectError || subError) && "border-danger")}
            value={subject}
            onChange={(e) => {
              setSubject(e.target.value);
              setSubError(null);
            }}
            placeholder="orders.> or orders.*.created"
            disabled={!connected}
          />
          {(subError || subjectError) && <span className="text-[11px] text-danger">{subError ?? subjectError}</span>}
        </div>
        {queueMode && (
          <Input
            aria-label="Queue group"
            className="w-40 font-mono text-xs"
            placeholder="queue group"
            value={queueGroup}
            onChange={(e) => setQueueGroup(e.target.value)}
          />
        )}
        <Button type="submit" size="sm" disabled={!connected || busy || Boolean(subjectError)} loading={busy}>
          <Radio className="h-3.5 w-3.5" /> Subscribe
        </Button>
        <Button
          type="button"
          size="sm"
          variant={queueMode ? "secondary" : "ghost"}
          aria-pressed={queueMode}
          title="Join a queue group. Messages delivered to Relay are taken away from other members of that group."
          onClick={() => setQueueMode(!queueMode)}
          disabled={!connected}
        >
          <Users className="h-3.5 w-3.5" /> Queue group
        </Button>
        <div className="ml-auto flex items-center gap-2">
          <Input aria-label="Filter messages" className="h-7 w-48 text-xs" placeholder="Filter subject, headers, payload" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <Button
            type="button"
            size="icon-sm"
            variant={followLatest ? "secondary" : "ghost"}
            aria-pressed={followLatest}
            aria-label="Follow latest message"
            title="Follow latest"
            onClick={() => {
              setFollowLatest(!followLatest);
              if (!followLatest) setSelectedMessage(null);
            }}
          >
            <ArrowDownToLine className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" size="icon-sm" variant="ghost" aria-label="Clear messages" title="Clear messages" onClick={() => clearMessages(connectionId)}>
            <Trash2 className="h-3.5 w-3.5" />
          </Button>
        </div>
      </form>

      {!connected && (
        <Banner
          tone="warning"
          className="border-x-0 border-t-0"
          action={
            status !== "reconnecting" && status !== "connecting" ? (
              <Button size="xs" onClick={() => void connect(connectionId)}>
                Connect
              </Button>
            ) : undefined
          }
        >
          {status === "reconnecting" ? "Reconnecting… subscriptions resume automatically." : `${conn.name} is ${status ?? "disconnected"}.`}
        </Banner>
      )}
      {queueMode && (
        <Banner tone="warning" className="border-x-0 border-t-0">
          Queue-group mode competes with real consumers in that group. Use observe mode to watch traffic without affecting delivery.
        </Banner>
      )}

      {subs.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-3 py-1.5">
          <Select aria-label="Show messages from" className="h-6 w-auto py-0 text-[11px]" value={subFilter} onChange={(e) => setSubFilter(e.target.value)}>
            <option value="all">All subscriptions</option>
            {subs.map((s) => (
              <option key={s.id} value={s.id}>
                {s.subject}
              </option>
            ))}
          </Select>
          {subs.map((s) => (
            <SubscriptionChip key={s.id} sub={s} onAction={(a) => void subAction(s, a)} />
          ))}
        </div>
      )}

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(320px,38%)]">
        <div ref={parentRef} className="min-h-0 overflow-auto border-r border-border" role="listbox" aria-label="Messages">
          {messages.length === 0 ? (
            <EmptyState
              icon={Radio}
              title={subs.length === 0 ? "No subscriptions" : filter ? "No messages match the filter" : "Waiting for messages…"}
              description={
                subs.length === 0
                  ? "Subscribe to a subject pattern to watch live traffic. Core NATS has no subject catalog, so wildcards like > are the way to discover subjects."
                  : undefined
              }
            />
          ) : (
            <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
              {virtualizer.getVirtualItems().map((row) => {
                const m = messages[row.index];
                const p = preview(m);
                const active = selected?.id === m.id;
                return (
                  <button
                    type="button"
                    role="option"
                    aria-selected={active}
                    key={m.id}
                    className={cn(
                      "absolute left-0 flex w-full items-center gap-3 border-b border-border/50 px-3 text-left text-xs",
                      active ? "bg-accent/12" : "hover:bg-bg-hover",
                    )}
                    style={{ height: row.size, transform: `translateY(${row.start}px)` }}
                    onClick={() => setSelectedMessage(m.id)}
                  >
                    <span className="tabular w-[74px] shrink-0 text-fg-subtle">{fmtTime(m.receivedAt, tz)}</span>
                    <span className="w-52 shrink-0 truncate font-mono text-accent">{m.metadata.subject}</span>
                    <span className={cn("min-w-0 flex-1 truncate", p.binary ? "italic text-fg-subtle" : "text-fg-muted")}>{p.text}</span>
                    {m.truncated && <Badge tone="warning">truncated</Badge>}
                    <span className="tabular shrink-0 text-fg-subtle">{formatBytes(m.sizeBytes)}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>
        <MessageDetail message={selected} tab={detailTab} setTab={setDetailTab} view={payloadView} setView={setPayloadView} tz={tz} />
      </div>
      <div className="flex h-6 shrink-0 items-center gap-3 border-t border-border px-3 text-[11px] text-fg-subtle">
        <span>
          <span className="tabular">{messages.length}</span> shown
        </span>
        {subs.some((s) => s.droppedCount > 0) && (
          <span className="text-warning">
            <span className="tabular">{subs.reduce((n, s) => n + s.droppedCount, 0)}</span> not displayed (paused or buffer full)
          </span>
        )}
      </div>
    </div>
  );
}

function SubscriptionChip({ sub, onAction }: { sub: SubscriptionStatus; onAction: (a: "pause" | "resume" | "unsubscribe") => void }) {
  const paused = sub.status === "paused";
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center gap-1.5 rounded-md border pl-2 pr-0.5 text-[11px]",
        sub.status === "error" ? "border-danger/40 bg-danger/10" : paused ? "border-warning/40 bg-warning/10" : "border-border bg-bg-panel",
      )}
      title={sub.error ?? sub.details?.note}
    >
      <span className="font-mono">{sub.subject}</span>
      {sub.mode === "queue" && <span className="text-fg-subtle">queue</span>}
      <span className="tabular text-fg-subtle">{sub.messageCount}</span>
      {sub.droppedCount > 0 && <span className="tabular text-warning">−{sub.droppedCount}</span>}
      <button
        type="button"
        className="rounded p-0.5 hover:bg-bg-hover"
        aria-label={paused ? `Resume ${sub.subject}` : `Pause ${sub.subject}`}
        title={paused ? "Resume display" : "Pause display. Messages arriving while paused are counted, not shown."}
        onClick={() => onAction(paused ? "resume" : "pause")}
      >
        {paused ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
      </button>
      <button type="button" className="rounded p-0.5 hover:bg-bg-hover" aria-label={`Unsubscribe ${sub.subject}`} title="Unsubscribe" onClick={() => onAction("unsubscribe")}>
        <Square className="h-3 w-3" />
      </button>
    </span>
  );
}

function MessageDetail({
  message,
  tab,
  setTab,
  view,
  setView,
  tz,
}: {
  message?: InboundMessage;
  tab: DetailTab;
  setTab: (t: DetailTab) => void;
  view: PayloadView;
  setView: (v: PayloadView) => void;
  tz: "local" | "utc";
}) {
  const decoded = useMemo(() => {
    if (!message) return null;
    const bytes = decodeBytes(message.payloadBase64);
    const binary = isBinary(bytes);
    const text = binary ? "" : textDecoder.decode(bytes);
    const pretty = binary ? "" : tryPrettyJson(text);
    return { bytes, binary, text, pretty, json: !binary && pretty !== text };
  }, [message]);

  if (!message || !decoded) {
    return (
      <div className="bg-bg-panel">
        <EmptyState compact title="No message selected" description="Select a message to inspect its payload and headers." />
      </div>
    );
  }

  const effective: PayloadView = view === "auto" ? (decoded.binary ? "hex" : "text") : view;
  const body =
    effective === "hex"
      ? hexDump(decoded.bytes)
      : effective === "base64"
        ? message.payloadBase64
        : decoded.binary
          ? "Binary payload. Switch to Hex or Base64."
          : decoded.json
            ? decoded.pretty
            : decoded.text;
  const headerEntries = Object.entries(message.headers ?? {});

  return (
    <div className="flex min-h-0 flex-col bg-bg-panel">
      <div className="border-b border-border px-3 py-2">
        <div className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-accent">{message.metadata.subject}</span>
          <CopyButton text={message.metadata.subject ?? ""} label="Copy subject" />
        </div>
        <div className="mt-0.5 text-[11px] text-fg-subtle">
          {fmtDateTime(message.receivedAt, tz)} · {formatBytes(message.sizeBytes)}
          {decoded.json && " · JSON"}
          {decoded.binary && " · binary"}
        </div>
      </div>
      <Tabs
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "payload", label: "Payload" },
          { value: "headers", label: "Headers", count: headerEntries.length },
          { value: "metadata", label: "Metadata" },
        ]}
      />
      {tab === "payload" && (
        <>
          <div className="flex items-center gap-1 border-b border-border px-2 py-1">
            {(["auto", "text", "hex", "base64"] as PayloadView[]).map((v) => (
              <button
                key={v}
                type="button"
                aria-pressed={view === v}
                onClick={() => setView(v)}
                className={cn("rounded px-2 py-0.5 text-[11px] capitalize", view === v ? "bg-bg-active text-fg" : "text-fg-subtle hover:text-fg")}
              >
                {v}
              </button>
            ))}
            <CopyButton className="ml-auto" text={effective === "base64" ? message.payloadBase64 : decoded.binary ? hexDump(decoded.bytes) : decoded.text} label="Copy payload" />
          </div>
          {message.truncated && (
            <Banner tone="warning" className="border-x-0 border-t-0">
              Payload truncated for display. The original was {formatBytes(message.sizeBytes)}.
            </Banner>
          )}
          <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11.5px] leading-relaxed">{body || "(empty)"}</pre>
        </>
      )}
      {tab === "headers" && (
        <div className="min-h-0 flex-1 overflow-auto p-3">
          {headerEntries.length === 0 ? (
            <p className="text-xs text-fg-subtle">No headers.</p>
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-left text-[11px] text-fg-subtle">
                  <th className="pb-1 font-medium">Name</th>
                  <th className="pb-1 font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {headerEntries.flatMap(([k, vals]) =>
                  vals.map((v, i) => (
                    <tr key={`${k}-${i}`} className="border-t border-border align-top">
                      <td className="py-1 pr-3 font-mono text-fg-muted">{k}</td>
                      <td className="break-all py-1 font-mono">{v}</td>
                    </tr>
                  )),
                )}
              </tbody>
            </table>
          )}
        </div>
      )}
      {tab === "metadata" && (
        <div className="min-h-0 flex-1 overflow-auto p-3">
          <KeyValue
            items={[
              ["Subject", <span className="font-mono">{message.metadata.subject}</span>],
              ["Reply to", message.metadata.replyTo ? <span className="font-mono">{message.metadata.replyTo}</span> : undefined],
              ["Stream", message.metadata.stream],
              ["Sequence", message.metadata.sequence],
              ["Received", fmtDateTime(message.receivedAt, tz)],
              ["Broker time", message.brokerTimestamp ? fmtDateTime(message.brokerTimestamp, tz) : undefined],
              ["Size", `${message.sizeBytes} bytes`],
              ["Truncated", message.truncated ? "Yes" : "No"],
              ["Acknowledgement", message.ackMode ?? "none"],
              ["Message ID", <span className="font-mono text-[11px]">{message.id}</span>],
            ]}
          />
        </div>
      )}
    </div>
  );
}
