package broker

import (
	"context"
	"encoding/json"
	"time"
)

type BrokerType string

const (
	BrokerNATS     BrokerType = "nats"
	BrokerRabbitMQ BrokerType = "rabbitmq"
	BrokerKafka    BrokerType = "kafka"
)

type EnvironmentKind string

const (
	EnvLocal       EnvironmentKind = "local"
	EnvDevelopment EnvironmentKind = "development"
	EnvStaging     EnvironmentKind = "staging"
	EnvProduction  EnvironmentKind = "production"
	EnvCustom      EnvironmentKind = "custom"
)

type ConnectionStatus string

const (
	StatusDisconnected  ConnectionStatus = "disconnected"
	StatusConnecting    ConnectionStatus = "connecting"
	StatusConnected     ConnectionStatus = "connected"
	StatusReconnecting  ConnectionStatus = "reconnecting"
	StatusError         ConnectionStatus = "error"
	StatusDisconnecting ConnectionStatus = "disconnecting"
)

type AuthMethod string

const (
	AuthNone     AuthMethod = "none"
	AuthUserPass AuthMethod = "user_pass"
	AuthToken    AuthMethod = "token"
	AuthNKey     AuthMethod = "nkey"
	AuthCreds    AuthMethod = "creds"
	AuthTLSCert  AuthMethod = "tls_cert"
)

type TLSConfig struct {
	Enabled            bool   `json:"enabled"`
	CACertPath         string `json:"caCertPath,omitempty"`
	ClientCertPath     string `json:"clientCertPath,omitempty"`
	ClientKeyPath      string `json:"clientKeyPath,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"` // always false in production UI; validated
	ServerName         string `json:"serverName,omitempty"`
}

type AuthConfig struct {
	Method    AuthMethod `json:"method"`
	Username  string     `json:"username,omitempty"`
	SecretRef string     `json:"secretRef,omitempty"`
	// EphemeralSecret is never persisted; used for connect/test when host supplies secret once.
	EphemeralSecret string `json:"ephemeralSecret,omitempty"`
	TokenRef        string `json:"tokenRef,omitempty"`
	NKeySeedRef     string `json:"nkeySeedRef,omitempty"`
	CredsPath       string `json:"credsPath,omitempty"`
}

type ReconnectPolicy struct {
	Enabled          bool `json:"enabled"`
	InitialBackoffMs int  `json:"initialBackoffMs"`
	MaxBackoffMs     int  `json:"maxBackoffMs"`
	MaxAttempts      int  `json:"maxAttempts"` // 0 = unlimited
}

// UnlimitedReconnects reports whether the policy retries forever.
func (r ReconnectPolicy) UnlimitedReconnects() bool { return r.MaxAttempts <= 0 }

type ConnectionProfile struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	BrokerType      BrokerType        `json:"brokerType"`
	Environment     EnvironmentKind   `json:"environment"`
	EnvironmentName string            `json:"environmentName,omitempty"`
	Servers         []string          `json:"servers"`
	Auth            AuthConfig        `json:"auth"`
	TLS             TLSConfig         `json:"tls"`
	TimeoutMs       int               `json:"timeoutMs"`
	Reconnect       ReconnectPolicy   `json:"reconnect"`
	Tags            []string          `json:"tags,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	ReadOnly        bool              `json:"readOnly"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	LastConnectedAt *time.Time        `json:"lastConnectedAt,omitempty"`
	LastStatus      ConnectionStatus  `json:"lastStatus"`
}

type HealthStatus struct {
	Healthy bool              `json:"healthy"`
	Message string            `json:"message,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

type Capability string

const (
	CapConnect      Capability = "connect"
	CapPublish      Capability = "publish"
	CapSubscribe    Capability = "subscribe"
	CapRequestReply Capability = "request_reply"
	CapExplore      Capability = "explore"
	CapJetStream    Capability = "jetstream"
	CapQueueManage  Capability = "queue_manage"
	CapMetrics      Capability = "metrics"
	CapHistory      Capability = "history"
)

type BrokerCapabilities struct {
	BrokerType   BrokerType   `json:"brokerType"`
	Capabilities []Capability `json:"capabilities"`
	Limitations  []string     `json:"limitations,omitempty"`
}

type MessageHeaders map[string][]string

type MessageMetadata struct {
	Subject     string `json:"subject,omitempty"`
	ReplyTo     string `json:"replyTo,omitempty"`
	Stream      string `json:"stream,omitempty"`
	Consumer    string `json:"consumer,omitempty"`
	Sequence    uint64 `json:"sequence,omitempty"`
	Partition   int32  `json:"partition,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	Key         string `json:"key,omitempty"`
	Exchange    string `json:"exchange,omitempty"`
	RoutingKey  string `json:"routingKey,omitempty"`
	Queue       string `json:"queue,omitempty"`
	ContentType string `json:"contentType,omitempty"`
}

type InboundMessage struct {
	ID              string          `json:"id"`
	ConnectionID    string          `json:"connectionId"`
	SubscriptionID  string          `json:"subscriptionId,omitempty"`
	BrokerType      BrokerType      `json:"brokerType"`
	ReceivedAt      time.Time       `json:"receivedAt"`
	BrokerTimestamp *time.Time      `json:"brokerTimestamp,omitempty"`
	Headers         MessageHeaders  `json:"headers,omitempty"`
	Metadata        MessageMetadata `json:"metadata"`
	PayloadBase64   string          `json:"payloadBase64"`
	SizeBytes       int             `json:"sizeBytes"`
	Truncated       bool            `json:"truncated"`
	AckMode         string          `json:"ackMode,omitempty"` // none | manual | auto
}

type OutboundMessage struct {
	ConnectionID  string         `json:"connectionId"`
	Subject       string         `json:"subject,omitempty"`
	Topic         string         `json:"topic,omitempty"`
	Exchange      string         `json:"exchange,omitempty"`
	RoutingKey    string         `json:"routingKey,omitempty"`
	Key           string         `json:"key,omitempty"`
	Partition     *int32         `json:"partition,omitempty"`
	Headers       MessageHeaders `json:"headers,omitempty"`
	PayloadBase64 string         `json:"payloadBase64"`
	ContentType   string         `json:"contentType,omitempty"`
	ReplyTo       string         `json:"replyTo,omitempty"`
}

type PublishResult struct {
	OK        bool   `json:"ok"`
	MessageID string `json:"messageId,omitempty"`
	Error     string `json:"error,omitempty"`
}

type SubscribeMode string

const (
	SubscribeObserve  SubscribeMode = "observe" // no ack / diagnostic
	SubscribeConsume  SubscribeMode = "consume" // may ack
	SubscribeQueueGrp SubscribeMode = "queue"   // queue group
)

type SubscribeRequest struct {
	ConnectionID string        `json:"connectionId"`
	Subject      string        `json:"subject"`
	QueueGroup   string        `json:"queueGroup,omitempty"`
	Mode         SubscribeMode `json:"mode"`
	MaxInFlight  int           `json:"maxInFlight,omitempty"`
	// OnDrop is called by adapters when a message is discarded because the
	// delivery buffer is full. Never serialized.
	OnDrop func() `json:"-"`
}

type SubscriptionStatus struct {
	ID           string            `json:"id"`
	ConnectionID string            `json:"connectionId"`
	Subject      string            `json:"subject"`
	Status       string            `json:"status"` // active | paused | stopped | error
	Mode         SubscribeMode     `json:"mode"`
	MessageCount int64             `json:"messageCount"`
	DroppedCount int64             `json:"droppedCount"`
	Error        string            `json:"error,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
}

type ResourceKind string

const (
	ResourceSubject  ResourceKind = "subject"
	ResourceStream   ResourceKind = "stream"
	ResourceConsumer ResourceKind = "consumer"
	ResourceExchange ResourceKind = "exchange"
	ResourceQueue    ResourceKind = "queue"
	ResourceTopic    ResourceKind = "topic"
	ResourceGroup    ResourceKind = "consumer_group"
)

type ResourceOrigin string

const (
	OriginManual     ResourceOrigin = "manual"
	OriginObserved   ResourceOrigin = "observed"
	OriginInferred   ResourceOrigin = "inferred"
	OriginDiscovered ResourceOrigin = "discovered"
)

type ResourceDescriptor struct {
	ID           string            `json:"id"`
	ConnectionID string            `json:"connectionId"`
	Kind         ResourceKind      `json:"kind"`
	Name         string            `json:"name"`
	Origin       ResourceOrigin    `json:"origin"`
	ParentID     string            `json:"parentId,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Config struct {
	Profile ConnectionProfile
	// ResolvedSecrets maps logical keys (password, token, nkey) to values.
	ResolvedSecrets map[string]string
	// OnStatus receives runtime transitions after Connect succeeds
	// (reconnecting, connected, disconnected, error). Optional.
	OnStatus func(status ConnectionStatus, detail string)
}

// BrokerAdapter is the core adapter contract.
type BrokerAdapter interface {
	Type() BrokerType
	Capabilities() BrokerCapabilities
	Connect(ctx context.Context, cfg Config) error
	Disconnect(ctx context.Context) error
	Health(ctx context.Context) (HealthStatus, error)
	Publish(ctx context.Context, msg OutboundMessage) (PublishResult, error)
	Subscribe(ctx context.Context, subID string, req SubscribeRequest) (<-chan InboundMessage, error)
}

// RequestReply is optional.
type RequestReply interface {
	Request(ctx context.Context, msg OutboundMessage, timeout time.Duration) (*InboundMessage, error)
}

// ResourceExplorer is optional.
type ResourceExplorer interface {
	ListResources(ctx context.Context) ([]ResourceDescriptor, error)
}

// JetStreamExplorer is NATS-specific optional capability.
type JetStreamExplorer interface {
	ListStreams(ctx context.Context) ([]json.RawMessage, error)
	GetStream(ctx context.Context, name string) (json.RawMessage, error)
	ListConsumers(ctx context.Context, stream string) ([]json.RawMessage, error)
	GetMessage(ctx context.Context, stream string, seq uint64) (*InboundMessage, error)
}
