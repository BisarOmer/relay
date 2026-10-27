import { ChevronLeft, ChevronRight, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CopyButton, EmptyState, KeyValue, PageHeader, SectionHeader, Spinner, Stat, Tabs } from "@/components/ui/misc";
import { engineRequest, errorMessage } from "@/lib/engine";
import { fmtBytes, fmtDateTime, fmtDurationMs, fmtNumber, fmtRelative } from "@/lib/format";
import { decodeBytes, hexDump, isBinary, tryPrettyJson } from "@/lib/utils";
import { useAppStore } from "@/stores/app-store";
import type { InboundMessage } from "@/types/models";

interface StreamInfo {
  config: {
    name: string;
    description?: string;
    subjects?: string[];
    retention?: string;
    storage?: string;
    discard?: string;
    num_replicas?: number;
    max_consumers?: number;
    max_msgs?: number;
    max_bytes?: number;
    max_age?: number;
    max_msg_size?: number;
    max_msgs_per_subject?: number;
    duplicate_window?: number;
    sealed?: boolean;
    deny_delete?: boolean;
    deny_purge?: boolean;
    allow_direct?: boolean;
  };
  created?: string;
  state: {
    messages: number;
    bytes: number;
    first_seq: number;
    first_ts?: string;
    last_seq: number;
    last_ts?: string;
    consumer_count: number;
    num_deleted?: number;
    num_subjects?: number;
  };
  cluster?: { name?: string; leader?: string; replicas?: { name: string; current: boolean; active: number; lag?: number; offline?: boolean }[] };
}

interface ConsumerInfo {
  name: string;
  created?: string;
  config: {
    durable_name?: string;
    description?: string;
    deliver_policy?: string;
    ack_policy?: string;
    filter_subject?: string;
    filter_subjects?: string[];
    max_ack_pending?: number;
    deliver_subject?: string;
  };
  delivered?: { consumer_seq: number; stream_seq: number; last_active?: string };
  ack_floor?: { consumer_seq: number; stream_seq: number };
  num_ack_pending: number;
  num_redelivered: number;
  num_waiting: number;
  num_pending: number;
  push_bound?: boolean;
}

/** JetStream uses -1 / 0 for "no limit". */
function limit(v: number | undefined, fmt: (n: number) => string) {
  if (v === undefined) return undefined;
  return v <= 0 ? "Unlimited" : fmt(v);
}

const NS_PER_MS = 1_000_000;

export function StreamView({ connectionId, stream }: { connectionId: string; stream: string }) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const connected = useAppStore((s) => s.runtimeStatus[connectionId] === "connected");
  const [info, setInfo] = useState<StreamInfo | null>(null);
  const [consumers, setConsumers] = useState<ConsumerInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [tab, setTab] = useState<"overview" | "consumers" | "messages">("overview");
  const [loadedAt, setLoadedAt] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [s, c] = await Promise.all([
        engineRequest<StreamInfo>("jetstream.streams.get", { connectionId, name: stream }),
        engineRequest<{ consumers: ConsumerInfo[] }>("jetstream.consumers.list", { connectionId, stream }),
      ]);
      setInfo(s);
      setConsumers(c.consumers ?? []);
      setError(null);
      setLoadedAt(new Date().toISOString());
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  }, [connectionId, stream]);

  useEffect(() => {
    if (connected) void load();
  }, [connected, load]);

  if (!connected && !info) {
    return <EmptyState title="Not connected" description="Connect to load this stream." />;
  }
  if (!info) {
    return error ? (
      <EmptyState title="Could not load stream" description={error} action={<Button size="sm" onClick={() => void load()}>Retry</Button>} />
    ) : (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Loading stream…" />
      </div>
    );
  }

  const c = info.config;
  const st = info.state;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title={stream}
        description={c.description || (c.subjects?.length ? <span className="font-mono">{c.subjects.join(", ")}</span> : "No subjects (mirror or source stream)")}
        meta={
          <>
            <Badge>{c.storage ?? "storage unknown"}</Badge>
            <Badge>{c.retention ?? "retention unknown"}</Badge>
            {c.sealed && <Badge tone="warning">sealed</Badge>}
          </>
        }
        actions={
          <>
            {loadedAt && <span className="text-[11px] text-fg-subtle">Loaded {fmtRelative(loadedAt)}</span>}
            <Button variant="outline" size="sm" loading={loading} disabled={!connected} onClick={() => void load()}>
              <RefreshCw className="h-3.5 w-3.5" /> Refresh
            </Button>
          </>
        }
      />
      {error && <div className="border-b border-danger/30 bg-danger/10 px-5 py-1.5 text-xs text-danger">Refresh failed: {error}</div>}
      <Tabs
        className="px-3"
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "overview", label: "Overview" },
          { value: "consumers", label: "Consumers", count: consumers?.length },
          { value: "messages", label: "Messages" },
        ]}
      />
      <div className="min-h-0 flex-1 overflow-auto">
        {tab === "overview" && (
          <div className="space-y-5 p-5">
            <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
              <Stat label="Messages" value={fmtNumber(st.messages)} />
              <Stat label="Size" value={fmtBytes(st.bytes)} />
              <Stat label="Consumers" value={fmtNumber(st.consumer_count)} />
              <Stat label="Subjects" value={st.num_subjects !== undefined ? fmtNumber(st.num_subjects) : null} unknownReason="Not reported by the server" />
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <Card className="p-4">
                <SectionHeader title="State" className="mb-3" />
                <KeyValue
                  items={[
                    ["First sequence", fmtNumber(st.first_seq)],
                    ["First message", st.first_ts && st.messages > 0 ? fmtDateTime(st.first_ts, tz) : "—"],
                    ["Last sequence", fmtNumber(st.last_seq)],
                    ["Last message", st.last_ts && st.messages > 0 ? fmtDateTime(st.last_ts, tz) : "—"],
                    ["Deleted", st.num_deleted !== undefined ? fmtNumber(st.num_deleted) : undefined],
                    ["Created", info.created ? fmtDateTime(info.created, tz) : undefined],
                  ]}
                />
              </Card>
              <Card className="p-4">
                <SectionHeader title="Limits" className="mb-3" />
                <KeyValue
                  items={[
                    ["Max messages", limit(c.max_msgs, fmtNumber)],
                    ["Max bytes", limit(c.max_bytes, fmtBytes)],
                    ["Max age", limit(c.max_age, (n) => fmtDurationMs(n / NS_PER_MS))],
                    ["Max message size", limit(c.max_msg_size, fmtBytes)],
                    ["Max per subject", limit(c.max_msgs_per_subject, fmtNumber)],
                    ["Max consumers", limit(c.max_consumers, fmtNumber)],
                    ["Discard policy", c.discard],
                    ["Duplicate window", c.duplicate_window ? fmtDurationMs(c.duplicate_window / NS_PER_MS) : undefined],
                    ["Replicas", c.num_replicas],
                  ]}
                />
              </Card>
            </div>
            {info.cluster && (
              <Card className="p-4">
                <SectionHeader title="Cluster" description={info.cluster.name} className="mb-3" />
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-left text-[11px] text-fg-subtle">
                      <th className="pb-1 font-medium">Server</th>
                      <th className="pb-1 font-medium">Role</th>
                      <th className="pb-1 font-medium">State</th>
                      <th className="pb-1 text-right font-medium">Lag</th>
                      <th className="pb-1 text-right font-medium">Last seen</th>
                    </tr>
                  </thead>
                  <tbody>
                    {info.cluster.leader && (
                      <tr className="border-t border-border">
                        <td className="py-1.5 font-mono">{info.cluster.leader}</td>
                        <td>
                          <Badge tone="accent">leader</Badge>
                        </td>
                        <td>—</td>
                        <td className="tabular text-right">—</td>
                        <td className="text-right">—</td>
                      </tr>
                    )}
                    {(info.cluster.replicas ?? []).map((r) => (
                      <tr key={r.name} className="border-t border-border">
                        <td className="py-1.5 font-mono">{r.name}</td>
                        <td>replica</td>
                        <td>
                          <Badge tone={r.offline ? "danger" : r.current ? "success" : "warning"}>{r.offline ? "offline" : r.current ? "current" : "catching up"}</Badge>
                        </td>
                        <td className="tabular text-right">{r.lag !== undefined ? fmtNumber(r.lag) : "—"}</td>
                        <td className="tabular text-right">{fmtDurationMs(r.active / NS_PER_MS)} ago</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </Card>
            )}
          </div>
        )}
        {tab === "consumers" && <ConsumersTable consumers={consumers ?? []} tz={tz} />}
        {tab === "messages" && <MessageBrowser connectionId={connectionId} stream={stream} first={st.first_seq} last={st.last_seq} empty={st.messages === 0} />}
      </div>
    </div>
  );
}

function ConsumersTable({ consumers, tz }: { consumers: ConsumerInfo[]; tz: "local" | "utc" }) {
  if (consumers.length === 0) return <EmptyState title="No consumers" description="Nothing is reading from this stream." />;
  return (
    <div className="p-5">
      <div className="overflow-auto rounded-lg border border-border">
        <table className="w-full text-xs">
          <thead className="bg-bg-panel text-left text-[11px] text-fg-subtle">
            <tr>
              <th className="px-3 py-2 font-medium">Name</th>
              <th className="px-3 py-2 font-medium">Type</th>
              <th className="px-3 py-2 font-medium">Filter</th>
              <th className="px-3 py-2 font-medium">Ack policy</th>
              <th className="px-3 py-2 text-right font-medium">Pending</th>
              <th className="px-3 py-2 text-right font-medium">Ack pending</th>
              <th className="px-3 py-2 text-right font-medium">Redelivered</th>
              <th className="px-3 py-2 text-right font-medium">Waiting</th>
              <th className="px-3 py-2 text-right font-medium">Last active</th>
            </tr>
          </thead>
          <tbody>
            {consumers.map((c) => {
              const ackLimit = c.config.max_ack_pending ?? 0;
              const ackNear = ackLimit > 0 && c.num_ack_pending >= ackLimit * 0.8;
              return (
                <tr key={c.name} className="border-t border-border">
                  <td className="px-3 py-2 font-mono">
                    {c.name}
                    {c.config.description && <div className="font-sans text-[11px] text-fg-subtle">{c.config.description}</div>}
                  </td>
                  <td className="px-3 py-2">
                    {c.config.durable_name ? "durable" : "ephemeral"} · {c.config.deliver_subject ? "push" : "pull"}
                  </td>
                  <td className="px-3 py-2 font-mono">{c.config.filter_subject || c.config.filter_subjects?.join(", ") || "—"}</td>
                  <td className="px-3 py-2">{c.config.ack_policy ?? "—"}</td>
                  <td className="tabular px-3 py-2 text-right">{fmtNumber(c.num_pending)}</td>
                  <td className={`tabular px-3 py-2 text-right ${ackNear ? "text-warning" : ""}`} title={ackLimit > 0 ? `Limit ${ackLimit}` : undefined}>
                    {fmtNumber(c.num_ack_pending)}
                  </td>
                  <td className={`tabular px-3 py-2 text-right ${c.num_redelivered > 0 ? "text-warning" : ""}`}>{fmtNumber(c.num_redelivered)}</td>
                  <td className="tabular px-3 py-2 text-right">{fmtNumber(c.num_waiting)}</td>
                  <td className="px-3 py-2 text-right text-fg-subtle">
                    {c.delivered?.last_active ? <span title={fmtDateTime(c.delivered.last_active, tz)}>{fmtRelative(c.delivered.last_active)}</span> : "never"}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function MessageBrowser({ connectionId, stream, first, last, empty }: { connectionId: string; stream: string; first: number; last: number; empty: boolean }) {
  const tz = useAppStore((s) => s.preferences.timezone);
  const [seqInput, setSeqInput] = useState(String(last || 1));
  const [msg, setMsg] = useState<InboundMessage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchSeq = useCallback(
    async (seq: number) => {
      setSeqInput(String(seq));
      setLoading(true);
      setError(null);
      try {
        setMsg(await engineRequest<InboundMessage>("jetstream.messages.get", { connectionId, stream, sequence: seq }));
      } catch (e) {
        setMsg(null);
        setError(errorMessage(e));
      } finally {
        setLoading(false);
      }
    },
    [connectionId, stream],
  );

  useEffect(() => {
    if (!empty && last > 0) void fetchSeq(last);
  }, [empty, last, fetchSeq]);

  if (empty) return <EmptyState title="Stream is empty" />;

  const seq = Number(seqInput);
  const validSeq = Number.isInteger(seq) && seq >= 1;
  const bytes = msg ? decodeBytes(msg.payloadBase64) : null;
  const binary = bytes ? isBinary(bytes) : false;
  const body = bytes ? (binary ? hexDump(bytes) : tryPrettyJson(new TextDecoder().decode(bytes))) : "";

  return (
    <div className="space-y-4 p-5">
      <p className="text-xs text-fg-muted">
        Reads a stored message by sequence. This does not create a consumer and does not acknowledge anything. Sequences{" "}
        <span className="tabular">{fmtNumber(first)}</span>–<span className="tabular">{fmtNumber(last)}</span>.
      </p>
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (validSeq) void fetchSeq(seq);
        }}
      >
        <Button type="button" size="icon-sm" variant="outline" aria-label="Previous message" disabled={!validSeq || seq <= first || loading} onClick={() => void fetchSeq(seq - 1)}>
          <ChevronLeft className="h-4 w-4" />
        </Button>
        <Input aria-label="Sequence" inputMode="numeric" className="tabular w-32" value={seqInput} onChange={(e) => setSeqInput(e.target.value)} />
        <Button type="button" size="icon-sm" variant="outline" aria-label="Next message" disabled={!validSeq || seq >= last || loading} onClick={() => void fetchSeq(seq + 1)}>
          <ChevronRight className="h-4 w-4" />
        </Button>
        <Button type="submit" size="sm" disabled={!validSeq} loading={loading}>
          Load
        </Button>
      </form>
      {error && <p className="text-xs text-danger">{error.includes("not found") ? `Sequence ${seqInput} is not in the stream (deleted or out of range).` : error}</p>}
      {msg && (
        <Card className="overflow-hidden">
          <div className="border-b border-border p-3">
            <KeyValue
              items={[
                ["Subject", <span className="font-mono">{msg.metadata.subject}</span>],
                ["Sequence", msg.metadata.sequence],
                ["Stored at", msg.brokerTimestamp ? fmtDateTime(msg.brokerTimestamp, tz) : undefined],
                ["Size", fmtBytes(msg.sizeBytes)],
                ...Object.entries(msg.headers ?? {}).map(
                  ([k, v]) => [<span className="font-mono">{k}</span>, <span className="font-mono">{v.join(", ")}</span>] as [React.ReactNode, React.ReactNode],
                ),
              ]}
            />
          </div>
          <div className="relative">
            <CopyButton className="absolute right-2 top-2" text={body} label="Copy payload" />
            <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11.5px]">{body || "(empty)"}</pre>
          </div>
        </Card>
      )}
    </div>
  );
}
