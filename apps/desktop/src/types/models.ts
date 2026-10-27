import { z } from "zod";

export const BrokerTypeSchema = z.enum(["nats", "rabbitmq", "kafka"]);
export type BrokerType = z.infer<typeof BrokerTypeSchema>;

export const EnvironmentSchema = z.enum([
  "local",
  "development",
  "staging",
  "production",
  "custom",
]);
export type EnvironmentKind = z.infer<typeof EnvironmentSchema>;

export const ConnectionStatusSchema = z.enum([
  "disconnected",
  "connecting",
  "connected",
  "reconnecting",
  "error",
  "disconnecting",
]);
export type ConnectionStatus = z.infer<typeof ConnectionStatusSchema>;

export const AuthMethodSchema = z.enum([
  "none",
  "user_pass",
  "token",
  "nkey",
  "creds",
  "tls_cert",
]);

export const ConnectionProfileSchema = z.object({
  id: z.string(),
  name: z.string(),
  brokerType: BrokerTypeSchema,
  environment: EnvironmentSchema,
  environmentName: z.string().optional(),
  servers: z.array(z.string()).min(1),
  auth: z.object({
    method: AuthMethodSchema,
    username: z.string().optional(),
    secretRef: z.string().optional(),
    tokenRef: z.string().optional(),
    nkeySeedRef: z.string().optional(),
    credsPath: z.string().optional(),
  }),
  tls: z.object({
    enabled: z.boolean(),
    caCertPath: z.string().optional(),
    clientCertPath: z.string().optional(),
    clientKeyPath: z.string().optional(),
    insecureSkipVerify: z.boolean().default(false),
    serverName: z.string().optional(),
  }),
  timeoutMs: z.number().default(10000),
  reconnect: z.object({
    enabled: z.boolean(),
    initialBackoffMs: z.number(),
    maxBackoffMs: z.number(),
    maxAttempts: z.number(),
  }),
  tags: z.array(z.string()).optional(),
  metadata: z.record(z.string(), z.string()).optional(),
  readOnly: z.boolean().default(false),
  createdAt: z.string(),
  updatedAt: z.string(),
  lastConnectedAt: z.string().nullable().optional(),
  lastStatus: ConnectionStatusSchema,
});

export type ConnectionProfile = z.infer<typeof ConnectionProfileSchema>;

export interface InboundMessage {
  id: string;
  connectionId: string;
  subscriptionId?: string;
  brokerType: BrokerType;
  receivedAt: string;
  brokerTimestamp?: string;
  headers?: Record<string, string[]>;
  metadata: {
    subject?: string;
    replyTo?: string;
    stream?: string;
    sequence?: number;
    contentType?: string;
  };
  payloadBase64: string;
  sizeBytes: number;
  truncated: boolean;
  ackMode?: string;
}

export interface SubscriptionStatus {
  id: string;
  connectionId: string;
  subject: string;
  status: string;
  mode: string;
  messageCount: number;
  droppedCount: number;
  error?: string;
  details?: Record<string, string>;
}

export interface ResourceDescriptor {
  id: string;
  connectionId: string;
  kind: string;
  name: string;
  origin: string;
  parentId?: string;
  metadata?: Record<string, string>;
}

export type ActivityView = "explorer" | "messages" | "publisher" | "insights" | "monitoring" | "settings";

export type WorkspaceTab =
  | { id: string; kind: "welcome"; title: string }
  | { id: string; kind: "connection"; title: string; connectionId: string; mode?: "view" | "edit" | "new" }
  | { id: string; kind: "messages"; title: string; connectionId: string; subject?: string }
  | { id: string; kind: "publisher"; title: string; connectionId?: string }
  | { id: string; kind: "stream"; title: string; connectionId: string; stream: string }
  | { id: string; kind: "monitoring"; title: string; connectionId?: string };

export interface Preferences {
  theme: "dark" | "light" | "system";
  timezone: "local" | "utc";
  maxDisplayedMessages: number;
  sidebarCollapsed: boolean;
  density: "comfortable" | "compact";
}

export const DEFAULT_PREFERENCES: Preferences = {
  theme: "dark",
  timezone: "local",
  maxDisplayedMessages: 2000,
  sidebarCollapsed: false,
  density: "comfortable",
};

export interface HealthStatus {
  healthy: boolean;
  message?: string;
  details?: Record<string, string>;
}

export interface BrokerCapabilities {
  brokerType: BrokerType;
  capabilities: string[];
  limitations?: string[];
}
