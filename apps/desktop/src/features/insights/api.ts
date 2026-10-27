import { engineRequest } from "@/lib/engine";
import type { SeriesPoint as ChartSeriesPoint } from "@/lib/format";
import type {
  EntityDetail,
  EpochDiff,
  EpochSummary,
  Finding,
  FindingEvent,
  InsightsSystem,
  Overview,
  QueryResult,
  SavedQuery,
  SchemaObject,
  TopologySnapshot,
} from "./types";

export type SystemInput = Pick<
  InsightsSystem,
  "name" | "monitoringBaseUrl" | "collectIntervalMs" | "requestTimeoutMs" | "retentionEpochs" | "enabled" | "demo"
> & { id?: string };

export const insightsApi = {
  listSystems: () => engineRequest<{ systems: InsightsSystem[] }>("insights.systems.list").then((r) => r.systems ?? []),
  upsertSystem: (s: SystemInput) => engineRequest<InsightsSystem>("insights.systems.upsert", s),
  deleteSystem: (id: string) => engineRequest("insights.systems.delete", { id }),
  collect: (systemId: string) =>
    engineRequest<{ epochId: string; completeness: number; status: string; error?: string }>(
      "insights.collect",
      { systemId },
      120_000,
    ),
  overview: (systemId: string) => engineRequest<Overview>("insights.overview", { systemId }),
  topology: (systemId: string, epochId?: string) =>
    engineRequest<TopologySnapshot>("insights.topology", { systemId, epochId }),
  entity: (systemId: string, entityId: string, epochId?: string) =>
    engineRequest<EntityDetail>("insights.entity.get", { systemId, entityId, epochId }),
  epochs: (systemId: string, limit = 200) =>
    engineRequest<{ epochs: EpochSummary[] }>("insights.epochs.list", { systemId, limit }).then((r) => r.epochs ?? []),
  series: async (systemId: string, entityId: string, metric: string, limit = 240): Promise<ChartSeriesPoint[]> => {
    const r = await engineRequest<{ points: { epochId: string; collectedAt: string; value: number | null }[] }>(
      "insights.metrics.series",
      { systemId, entityId, metric, limit },
    );
    return (r.points ?? []).map((p) => ({ epochId: p.epochId, at: p.collectedAt, value: p.value }));
  },
  diff: (systemId: string, fromEpoch: string, toEpoch: string) =>
    engineRequest<EpochDiff>("insights.diff", { systemId, fromEpoch, toEpoch }),
  findings: (systemId: string, includeResolved: boolean) =>
    engineRequest<{ findings: Finding[] }>("insights.findings.list", { systemId, includeResolved }).then(
      (r) => r.findings ?? [],
    ),
  findingEvents: (id: string) =>
    engineRequest<{ events: FindingEvent[] }>("insights.findings.events", { id }).then((r) => r.events ?? []),
  ack: (id: string, acknowledged: boolean) => engineRequest("insights.findings.ack", { id, acknowledged }),
  resolve: (id: string) => engineRequest("insights.findings.resolve", { id }),
  query: (systemId: string, sql: string, limit: number, timeoutMs: number, queryId: string) =>
    engineRequest<QueryResult>("insights.query", { systemId, sql, limit, timeoutMs, queryId }, timeoutMs + 5_000),
  cancelQuery: (queryId: string) => engineRequest<{ cancelled: boolean }>("insights.query.cancel", { queryId }),
  schema: () => engineRequest<{ objects: SchemaObject[] }>("insights.schema").then((r) => r.objects ?? []),
  savedQueries: () => engineRequest<{ queries: SavedQuery[] }>("insights.queries.list").then((r) => r.queries ?? []),
  saveQuery: (q: { id?: string; name: string; sql: string }) => engineRequest<SavedQuery>("insights.queries.save", q),
  deleteQuery: (id: string) => engineRequest("insights.queries.delete", { id }),
};
