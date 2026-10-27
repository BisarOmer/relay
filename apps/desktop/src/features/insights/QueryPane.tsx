import { ChevronRight, Download, Play, Save, Square, Table2, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/input";
import { CopyButton, EmptyState, SectionHeader } from "@/components/ui/misc";
import { EngineError, errorMessage } from "@/lib/engine";
import { toCsv } from "@/lib/format";
import { cn, downloadText } from "@/lib/utils";
import { confirm, toast } from "@/stores/ui-store";
import { insightsApi } from "./api";
import { useAsync } from "./hooks";
import type { PaneProps } from "./InsightsWorkspace";
import type { QueryResult } from "./types";

const EXAMPLES: { name: string; sql: string }[] = [
  {
    name: "Entities by type (latest snapshot)",
    sql: "SELECT entity_type, count(*) AS n\nFROM insights_entities\nWHERE system_id = :system_id\n  AND epoch_id = (SELECT last_epoch_id FROM insights_systems WHERE id = :system_id)\nGROUP BY entity_type\nORDER BY n DESC",
  },
  {
    name: "Open findings",
    sql: "SELECT severity, title, status, last_observed_at\nFROM v_insights_findings_active\nWHERE system_id = :system_id\nORDER BY severity, last_observed_at DESC",
  },
  {
    name: "Snapshot completeness over time",
    sql: "SELECT collected_at, completeness, error_count\nFROM insights_epochs\nWHERE system_id = :system_id\nORDER BY collected_at DESC\nLIMIT 50",
  },
];

let querySeq = 0;

export function QueryPane({ system }: PaneProps) {
  const schema = useAsync(() => insightsApi.schema(), []);
  const saved = useAsync(() => insightsApi.savedQueries(), []);
  const [sql, setSql] = useState(EXAMPLES[0].sql);
  const [limit, setLimit] = useState("500");
  const [timeoutMs, setTimeoutMs] = useState("5000");
  const [running, setRunning] = useState<string | null>(null);
  const [result, setResult] = useState<QueryResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saveName, setSaveName] = useState("");
  const [savedId, setSavedId] = useState<string | null>(null);
  const [openObj, setOpenObj] = useState<string | null>(null);
  const editor = useRef<HTMLTextAreaElement>(null);

  const limitN = Number(limit);
  const timeoutN = Number(timeoutMs);
  const limitErr = !Number.isInteger(limitN) || limitN < 1 || limitN > 5000;
  const timeoutErr = !Number.isInteger(timeoutN) || timeoutN < 100 || timeoutN > 30000;

  async function run() {
    if (!sql.trim() || limitErr || timeoutErr || running) return;
    const id = `q-${Date.now()}-${++querySeq}`;
    setRunning(id);
    setError(null);
    try {
      setResult(await insightsApi.query(system.id, sql, limitN, timeoutN, id));
    } catch (e) {
      const err = EngineError.from(e);
      setResult(null);
      setError(err.code === "cancelled" ? "Query cancelled." : err.code === "timeout" ? `Query exceeded ${timeoutN} ms and was stopped.` : errorMessage(e));
    } finally {
      setRunning(null);
    }
  }

  async function cancel() {
    if (running) await insightsApi.cancelQuery(running).catch(() => undefined);
  }

  async function save() {
    if (!saveName.trim() || !sql.trim()) return;
    try {
      const q = await insightsApi.saveQuery({ id: savedId ?? undefined, name: saveName.trim(), sql });
      setSavedId(q.id);
      toast.success("Query saved", q.name);
      saved.reload();
    } catch (e) {
      toast.error("Could not save query", errorMessage(e));
    }
  }

  function insert(text: string) {
    const el = editor.current;
    if (!el) return setSql((s) => s + text);
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const next = sql.slice(0, start) + text + sql.slice(end);
    setSql(next);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(start + text.length, start + text.length);
    });
  }

  const exportName = `relay-${system.name.replace(/[^a-z0-9]+/gi, "-").toLowerCase()}-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "")}`;

  return (
    <div className="grid h-full min-h-0 grid-cols-[220px_1fr]">
      <aside className="flex min-h-0 flex-col border-r border-border bg-bg-panel">
        <div className="min-h-0 flex-1 overflow-auto p-2">
          <SectionHeader title="Schema" className="px-1 pb-1.5" description="Read-only views" />
          {schema.error && <p className="px-1 text-[11px] text-danger">{schema.error}</p>}
          <ul className="space-y-px">
            {(schema.data ?? []).map((o) => (
              <li key={o.name}>
                <div className="flex items-center rounded hover:bg-bg-hover">
                  <button type="button" aria-expanded={openObj === o.name} className="flex h-6 w-5 items-center justify-center text-fg-subtle" onClick={() => setOpenObj(openObj === o.name ? null : o.name)}>
                    <ChevronRight className={cn("h-3 w-3 transition-transform", openObj === o.name && "rotate-90")} />
                  </button>
                  <button type="button" className="flex min-w-0 flex-1 items-center gap-1.5 text-left font-mono text-[11px]" title={`Insert ${o.name}`} onClick={() => insert(o.name)}>
                    <Table2 className="h-3 w-3 shrink-0 text-fg-subtle" />
                    <span className="truncate">{o.name}</span>
                  </button>
                </div>
                {openObj === o.name && (
                  <ul className="mb-1 ml-6 border-l border-border pl-2">
                    {o.columns.map((c) => (
                      <li key={c}>
                        <button type="button" className="w-full truncate py-0.5 text-left font-mono text-[10.5px] text-fg-muted hover:text-fg" onClick={() => insert(c)}>
                          {c}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>
          <SectionHeader title="Saved" className="px-1 pb-1.5 pt-4" />
          {(saved.data ?? []).length === 0 && <p className="px-1 text-[11px] text-fg-subtle">No saved queries.</p>}
          <ul className="space-y-px">
            {(saved.data ?? []).map((q) => (
              <li key={q.id} className="group flex items-center rounded hover:bg-bg-hover">
                <button
                  type="button"
                  className={cn("min-w-0 flex-1 truncate px-1.5 py-1 text-left text-xs", savedId === q.id && "text-accent")}
                  onClick={() => {
                    setSql(q.sql);
                    setSaveName(q.name);
                    setSavedId(q.id);
                  }}
                >
                  {q.name}
                </button>
                <button
                  type="button"
                  aria-label={`Delete ${q.name}`}
                  className="p-1 text-fg-subtle opacity-0 hover:text-danger group-hover:opacity-100"
                  onClick={async () => {
                    if (!(await confirm({ title: `Delete “${q.name}”?`, confirmLabel: "Delete", tone: "danger" }))) return;
                    try {
                      await insightsApi.deleteQuery(q.id);
                      if (savedId === q.id) setSavedId(null);
                      saved.reload();
                    } catch (e) {
                      toast.error("Could not delete query", errorMessage(e));
                    }
                  }}
                >
                  <Trash2 className="h-3 w-3" />
                </button>
              </li>
            ))}
          </ul>
          <SectionHeader title="Examples" className="px-1 pb-1.5 pt-4" />
          <ul className="space-y-px">
            {EXAMPLES.map((q) => (
              <li key={q.name}>
                <button type="button" className="w-full truncate rounded px-1.5 py-1 text-left text-xs text-fg-muted hover:bg-bg-hover hover:text-fg" onClick={() => setSql(q.sql)}>
                  {q.name}
                </button>
              </li>
            ))}
          </ul>
        </div>
      </aside>
      <div className="flex min-h-0 flex-col">
        <div className="space-y-2 border-b border-border p-3">
          <Textarea
            ref={editor}
            aria-label="SQL query"
            rows={7}
            spellCheck={false}
            className="font-mono text-xs"
            value={sql}
            onChange={(e) => setSql(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                void run();
              }
            }}
          />
          <div className="flex flex-wrap items-center gap-2">
            {running ? (
              <Button size="sm" variant="danger" onClick={() => void cancel()}>
                <Square className="h-3 w-3" /> Cancel
              </Button>
            ) : (
              <Button size="sm" disabled={!sql.trim() || limitErr || timeoutErr} onClick={() => void run()} title="Run (⌘/Ctrl+Enter)">
                <Play className="h-3 w-3" /> Run
              </Button>
            )}
            <label className="flex items-center gap-1.5 text-[11px] text-fg-muted">
              Row limit
              <Input aria-invalid={limitErr} className={cn("tabular h-7 w-20 text-xs", limitErr && "border-danger")} value={limit} onChange={(e) => setLimit(e.target.value)} />
            </label>
            <label className="flex items-center gap-1.5 text-[11px] text-fg-muted">
              Timeout ms
              <Input aria-invalid={timeoutErr} className={cn("tabular h-7 w-20 text-xs", timeoutErr && "border-danger")} value={timeoutMs} onChange={(e) => setTimeoutMs(e.target.value)} />
            </label>
            <span className="text-[11px] text-fg-subtle">
              Use <code className="font-mono">:system_id</code> for the selected system.
            </span>
            <div className="ml-auto flex items-center gap-1.5">
              <Input aria-label="Saved query name" className="h-7 w-44 text-xs" placeholder="Name to save as" value={saveName} onChange={(e) => setSaveName(e.target.value)} />
              <Button size="sm" variant="outline" disabled={!saveName.trim() || !sql.trim()} onClick={() => void save()}>
                <Save className="h-3 w-3" /> {savedId ? "Update" : "Save"}
              </Button>
              {savedId && (
                <Button size="sm" variant="ghost" onClick={() => setSavedId(null)} title="Save as a new query instead">
                  New
                </Button>
              )}
            </div>
          </div>
          {(limitErr || timeoutErr) && <p className="text-[11px] text-danger">Row limit 1–5000, timeout 100–30000 ms.</p>}
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {error && (
            <div role="alert" className="m-3 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 font-mono text-xs text-danger">
              {error}
            </div>
          )}
          {!result && !error && (
            <EmptyState
              compact
              title="Read-only SQL over Insights data"
              description="Queries run on a read-only connection against the views listed on the left. Connection profiles, secrets and preferences are not queryable."
            />
          )}
          {result && (
            <div>
              <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-bg-panel px-3 py-1.5 text-[11px] text-fg-subtle">
                <span className="tabular">{result.rowCount} rows</span>
                <span>· {result.durationMs} ms</span>
                {result.truncated && <Badge tone="warning">truncated at {result.limit}</Badge>}
                <div className="ml-auto flex items-center gap-1">
                  <CopyButton text={toCsv(result.columns, result.rows)} label="Copy as CSV" />
                  <Button size="xs" variant="ghost" onClick={() => downloadText(`${exportName}.csv`, toCsv(result.columns, result.rows), "text/csv")}>
                    <Download className="h-3 w-3" /> CSV
                  </Button>
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() =>
                      downloadText(
                        `${exportName}.json`,
                        JSON.stringify(result.rows.map((r) => Object.fromEntries(result.columns.map((c, i) => [c, r[i]]))), null, 2),
                        "application/json",
                      )
                    }
                  >
                    <Download className="h-3 w-3" /> JSON
                  </Button>
                </div>
              </div>
              {result.rows.length === 0 ? (
                <p className="p-3 text-xs text-fg-subtle">Query returned no rows.</p>
              ) : (
                <table className="w-full border-collapse font-mono text-[11px]">
                  <thead className="sticky top-[29px] bg-bg-panel">
                    <tr>
                      {result.columns.map((c) => (
                        <th key={c} className="whitespace-nowrap border-b border-border px-3 py-1.5 text-left font-medium text-fg-muted">
                          {c}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {result.rows.map((r, i) => (
                      <tr key={i} className="hover:bg-bg-hover">
                        {r.map((v, j) => (
                          <td key={j} className="max-w-[360px] truncate border-b border-border/60 px-3 py-1" title={v === null ? "NULL" : String(v)}>
                            {v === null ? <span className="text-fg-subtle">NULL</span> : typeof v === "object" ? JSON.stringify(v) : String(v)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

