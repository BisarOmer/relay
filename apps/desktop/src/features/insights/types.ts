export type SystemStatus = "idle" | "collecting" | "ok" | "partial" | "unreachable" | "error" | (string & {});

export interface InsightsSystem {
  id: string;
  name: string;
  monitoringBaseUrl: string;
  collectIntervalMs: number;
  requestTimeoutMs: number;
  retentionEpochs: number;
  enabled: boolean;
  demo: boolean;
  lastEpochId?: string;
  lastCollectedAt?: string;
  lastSuccessAt?: string;
  lastStatus: SystemStatus;
  lastError?: string;
  running: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface EndpointStatus {
  endpoint: string;
  ok: boolean;
  httpCode?: number;
  error?: string;
  available: boolean;
  permissionDenied?: boolean;
  durationMs?: number;
}

export interface Overview {
  systemId: string;
  systemName: string;
  epochId?: string;
  collectedAt?: string;
  lastSuccessAt?: string;
  intervalMs: number;
  enabled: boolean;
  status: SystemStatus;
  lastError?: string;
  completeness: number;
  servers: number | null;
  connections: number | null;
  streams: number | null;
  consumers: number | null;
  slowConsumers: number | null;
  subscriptions: number | null;
  inMsgs: number | null;
  outMsgs: number | null;
  cpu: number | null;
  memBytes: number | null;
  jetstream: "enabled" | "disabled" | "unknown";
  serverEntityId?: string;
  version?: string;
  criticalFindings: number;
  warningFindings: number;
  infoFindings: number;
  collectionErrors: number;
  endpointStatus: EndpointStatus[];
  partial: boolean;
  demo: boolean;
  notes: string[];
}

export type EntityType =
  | "system"
  | "cluster"
  | "server"
  | "account"
  | "connection"
  | "route"
  | "gateway"
  | "leaf"
  | "stream"
  | "consumer"
  | (string & {});

export interface Entity {
  id: string;
  type: EntityType;
  systemId: string;
  nativeId?: string;
  displayName: string;
  parentId?: string;
  related?: string[];
  status: string;
  props: Record<string, unknown>;
  firstSeenAt?: string;
  lastSeenAt: string;
  freshness: "fresh" | "partial" | "stale" | (string & {});
}

export interface MetricSample {
  entityId: string;
  name: string;
  value: number | null;
  unit?: string;
  available: boolean;
}

export interface RelatedRef {
  id: string;
  displayName?: string;
  type?: string;
  inSnapshot: boolean;
}

export interface EntityDetail {
  entity: Entity;
  metrics: MetricSample[];
  children: Entity[];
  related: RelatedRef[];
  path: Entity[];
  findings: Finding[];
  epochId: string;
  collectedAt: string;
}

export interface TopologySnapshot {
  epochId: string;
  collectedAt: string | null;
  completeness: number;
  entities: Entity[];
  endpointStatus: EndpointStatus[];
}

export type FindingStatus = "new" | "active" | "resolved" | "reopened";

export interface Finding {
  id: string;
  systemId: string;
  checkId: string;
  entityId?: string;
  severity: "critical" | "warning" | "info";
  category: string;
  status: FindingStatus;
  title: string;
  summary: string;
  evidence: Record<string, unknown>;
  remediation?: string;
  steps?: string[];
  firstObservedAt: string;
  lastObservedAt: string;
  lastEpochId?: string;
  occurrenceCount: number;
  acknowledged: boolean;
  acknowledgedAt?: string;
  resolvedAt?: string;
  resolution?: string;
}

export interface FindingEvent {
  id: string;
  findingId: string;
  epochId?: string;
  eventType: string;
  observedAt: string;
  evidence?: Record<string, unknown>;
}

export interface EpochSummary {
  id: string;
  systemId: string;
  collectedAt: string;
  completeness: number;
  errorCount: number;
}

export interface DiffChange {
  entityId: string;
  entityType: string;
  displayName?: string;
  kind: "added" | "removed" | "missing_or_removed" | "changed";
  field?: string;
  before?: unknown;
  after?: unknown;
}

export interface EpochDiff {
  systemId: string;
  fromEpoch: string;
  toEpoch: string;
  fromAt: string;
  toAt: string;
  changes: DiffChange[];
  incomplete: boolean;
  notes: string[];
}

export interface QueryResult {
  columns: string[];
  rows: unknown[][];
  rowCount: number;
  truncated: boolean;
  limit: number;
  durationMs: number;
}

export interface SchemaObject {
  name: string;
  kind: "table" | "view";
  columns: string[];
}

export interface SavedQuery {
  id: string;
  name: string;
  sql: string;
  createdAt: string;
  updatedAt: string;
}

export interface SeriesPoint {
  epochId: string;
  at: string;
  value: number | null;
}
