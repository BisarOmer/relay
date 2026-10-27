package natsadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
	"github.com/relay-ide/relay/engine/internal/broker"
)

type Adapter struct {
	mu      sync.RWMutex
	closing atomic.Bool
	nc      *nats.Conn
	js      jetstream.JetStream
	cfg     broker.Config
	connID  string
}

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Type() broker.BrokerType { return broker.BrokerNATS }

func (a *Adapter) Capabilities() broker.BrokerCapabilities {
	return broker.BrokerCapabilities{
		BrokerType: broker.BrokerNATS,
		Capabilities: []broker.Capability{
			broker.CapConnect,
			broker.CapPublish,
			broker.CapSubscribe,
			broker.CapRequestReply,
			broker.CapExplore,
			broker.CapJetStream,
		},
		Limitations: []string{
			"Core NATS has no authoritative subject catalog; subjects are manual, saved, or observed.",
			"JetStream administration requires appropriate account permissions.",
		},
	}
}

func (a *Adapter) Connect(ctx context.Context, cfg broker.Config) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nc != nil {
		a.closing.Store(true)
		a.nc.Close()
		a.nc, a.js = nil, nil
	}
	a.closing.Store(false)

	opts, err := a.options(cfg)
	if err != nil {
		return err
	}
	url := strings.Join(cfg.Profile.Servers, ",")
	if url == "" {
		return broker.NewError(broker.ErrValidation, "at least one NATS server URL is required", false)
	}

	type result struct {
		nc  *nats.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		nc, err := nats.Connect(url, opts...)
		ch <- result{nc, err}
	}()

	select {
	case <-ctx.Done():
		// Do not leak a connection that completes after the caller gave up.
		go func() {
			if r := <-ch; r.nc != nil {
				r.nc.Close()
			}
		}()
		if ctx.Err() == context.Canceled {
			return broker.NewError(broker.ErrCancelled, "connection attempt cancelled", false)
		}
		return broker.Wrap(broker.ErrTimeout, "connection timed out", true, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return mapConnectError(r.err)
		}
		a.nc = r.nc
		a.cfg = cfg
		a.connID = cfg.Profile.ID
		if js, err := jetstream.New(r.nc); err == nil {
			a.js = js
		}
		return nil
	}
}

// reconnectDelay is exponential backoff with jitter, bounded by the profile.
func reconnectDelay(initial, maxBackoff time.Duration, attempts int) time.Duration {
	if initial <= 0 {
		initial = 500 * time.Millisecond
	}
	if maxBackoff < initial {
		maxBackoff = initial
	}
	d := initial
	for i := 1; i < attempts && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	jitter := time.Duration(rand.Int64N(int64(d)/5 + 1))
	return d - d/10 + jitter
}

func (a *Adapter) options(cfg broker.Config) ([]nats.Option, error) {
	timeout := time.Duration(cfg.Profile.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	notify := func(st broker.ConnectionStatus, detail string) {
		if cfg.OnStatus != nil {
			cfg.OnStatus(st, detail)
		}
	}
	opts := []nats.Option{
		nats.Name("relay-ide"),
		nats.Timeout(timeout),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if a.closing.Load() {
				return
			}
			detail := "connection lost"
			if err != nil {
				detail = "connection lost: " + err.Error()
			}
			if cfg.Profile.Reconnect.Enabled {
				notify(broker.StatusReconnecting, detail)
			} else {
				notify(broker.StatusDisconnected, detail)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			notify(broker.StatusConnected, "reconnected to "+nc.ConnectedUrlRedacted())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			if a.closing.Load() {
				return
			}
			detail := "connection closed"
			if err := nc.LastError(); err != nil {
				detail = "connection closed: " + err.Error()
			}
			notify(broker.StatusError, detail)
		}),
	}
	if r := cfg.Profile.Reconnect; r.Enabled {
		maxAttempts := r.MaxAttempts
		if r.UnlimitedReconnects() {
			maxAttempts = -1 // nats.go: negative means unlimited, 0 means none
		}
		initial := time.Duration(r.InitialBackoffMs) * time.Millisecond
		maxBackoff := time.Duration(r.MaxBackoffMs) * time.Millisecond
		opts = append(opts,
			nats.MaxReconnects(maxAttempts),
			nats.CustomReconnectDelay(func(attempts int) time.Duration {
				return reconnectDelay(initial, maxBackoff, attempts)
			}),
		)
	} else {
		opts = append(opts, nats.NoReconnect())
	}

	switch cfg.Profile.Auth.Method {
	case broker.AuthUserPass:
		pass := cfg.ResolvedSecrets["password"]
		if cfg.Profile.Auth.Username == "" {
			return nil, broker.NewError(broker.ErrValidation, "username is required", false)
		}
		opts = append(opts, nats.UserInfo(cfg.Profile.Auth.Username, pass))
	case broker.AuthToken:
		token := cfg.ResolvedSecrets["token"]
		if token == "" {
			return nil, broker.NewError(broker.ErrValidation, "token is required", false)
		}
		opts = append(opts, nats.Token(token))
	case broker.AuthNKey:
		seed := strings.TrimSpace(cfg.ResolvedSecrets["nkey"])
		if seed == "" {
			return nil, broker.NewError(broker.ErrValidation, "NKey seed required", false)
		}
		// Keep the seed in memory only; never write it to disk.
		kp, err := nkeys.FromSeed([]byte(seed))
		if err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid NKey seed", false)
		}
		pub, err := kp.PublicKey()
		if err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid NKey seed", false)
		}
		if !nkeys.IsValidPublicUserKey(pub) {
			return nil, broker.NewError(broker.ErrValidation, "NKey seed is not a user seed (expected SU...)", false)
		}
		opts = append(opts, nats.Nkey(pub, kp.Sign))
	case broker.AuthCreds:
		if cfg.Profile.Auth.CredsPath == "" {
			return nil, broker.NewError(broker.ErrValidation, "creds path required", false)
		}
		opts = append(opts, nats.UserCredentials(cfg.Profile.Auth.CredsPath))
	case broker.AuthTLSCert:
		if !cfg.Profile.TLS.Enabled || cfg.Profile.TLS.ClientCertPath == "" || cfg.Profile.TLS.ClientKeyPath == "" {
			return nil, broker.NewError(broker.ErrValidation, "TLS client certificate authentication requires TLS with a client certificate and key", false)
		}
	case broker.AuthNone, "":
	default:
		return nil, broker.NewError(broker.ErrValidation, "unsupported auth method for NATS", false)
	}

	if cfg.Profile.TLS.Enabled {
		tlsCfg, err := buildTLS(cfg.Profile.TLS)
		if err != nil {
			return nil, err
		}
		opts = append(opts, nats.Secure(tlsCfg))
	}
	return opts, nil
}

func buildTLS(c broker.TLSConfig) (*tls.Config, error) {
	if c.InsecureSkipVerify {
		return nil, broker.NewError(broker.ErrTLS, "insecure TLS skip-verify is not allowed", false)
	}
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: c.ServerName,
	}
	if c.CACertPath != "" {
		pem, err := os.ReadFile(c.CACertPath)
		if err != nil {
			return nil, broker.Wrap(broker.ErrTLS, "failed to read CA certificate", false, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, broker.NewError(broker.ErrTLS, "invalid CA certificate", false)
		}
		tlsCfg.RootCAs = pool
	}
	if c.ClientCertPath != "" && c.ClientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(c.ClientCertPath, c.ClientKeyPath)
		if err != nil {
			return nil, broker.Wrap(broker.ErrTLS, "failed to load client certificate", false, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}

func mapConnectError(err error) error {
	lower := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, nats.ErrAuthorization) || strings.Contains(lower, "authentication") || strings.Contains(lower, "authorization"):
		return broker.Wrap(broker.ErrAuth, "authentication failed", false, err)
	case strings.Contains(lower, "certificate") || strings.Contains(lower, "tls"):
		return broker.Wrap(broker.ErrTLS, "TLS handshake failed", false, err)
	case errors.Is(err, nats.ErrTimeout) || strings.Contains(lower, "timeout"):
		return broker.Wrap(broker.ErrTimeout, "connection timed out", true, err)
	case errors.Is(err, nats.ErrNoServers) || strings.Contains(lower, "connection refused") || strings.Contains(lower, "lookup"):
		return broker.Wrap(broker.ErrNetwork, "unable to reach NATS server", true, err)
	default:
		return broker.Wrap(broker.ErrBroker, "NATS connection failed", true, err)
	}
}

func (a *Adapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nc == nil {
		return nil
	}
	a.closing.Store(true)
	nc := a.nc
	a.nc, a.js = nil, nil
	if !nc.IsConnected() {
		nc.Close()
		return nil
	}
	closed := make(chan struct{})
	nc.SetClosedHandler(func(*nats.Conn) { close(closed) })
	if err := nc.Drain(); err != nil {
		nc.Close()
		return nil
	}
	select {
	case <-closed:
	case <-ctx.Done():
		nc.Close()
	case <-time.After(5 * time.Second):
		nc.Close()
	}
	return nil
}

func (a *Adapter) Health(ctx context.Context) (broker.HealthStatus, error) {
	a.mu.RLock()
	nc := a.nc
	a.mu.RUnlock()
	if nc == nil {
		return broker.HealthStatus{Healthy: false, Message: "disconnected"}, nil
	}
	if !nc.IsConnected() {
		msg := "disconnected"
		if nc.IsReconnecting() {
			msg = "reconnecting"
		}
		return broker.HealthStatus{Healthy: false, Message: msg}, nil
	}
	details := map[string]string{
		"server":     nc.ConnectedUrlRedacted(),
		"serverName": nc.ConnectedServerName(),
		"serverId":   nc.ConnectedServerId(),
		"version":    nc.ConnectedServerVersion(),
		"jetstream":  "unknown",
	}
	if rtt, err := nc.RTT(); err == nil {
		details["rttMs"] = fmt.Sprintf("%.2f", float64(rtt.Microseconds())/1000)
	}
	a.mu.RLock()
	js := a.js
	a.mu.RUnlock()
	if js != nil {
		jctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := js.AccountInfo(jctx)
		cancel()
		switch {
		case err == nil:
			details["jetstream"] = "enabled"
		case errors.Is(err, jetstream.ErrJetStreamNotEnabled), errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount):
			details["jetstream"] = "disabled"
		}
	}
	return broker.HealthStatus{Healthy: true, Message: "connected", Details: details}, nil
}

func (a *Adapter) conn() (*nats.Conn, string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.nc == nil {
		return nil, "", broker.NewError(broker.ErrNetwork, "not connected", true)
	}
	if !a.nc.IsConnected() {
		if a.nc.IsReconnecting() {
			return nil, "", broker.NewError(broker.ErrNetwork, "connection is reconnecting; try again shortly", true)
		}
		return nil, "", broker.NewError(broker.ErrNetwork, "not connected", true)
	}
	return a.nc, a.connID, nil
}

func (a *Adapter) Publish(ctx context.Context, msg broker.OutboundMessage) (broker.PublishResult, error) {
	nc, _, err := a.conn()
	if err != nil {
		return broker.PublishResult{}, err
	}
	if err := ValidateSubject(msg.Subject, false); err != nil {
		return broker.PublishResult{}, err
	}
	if msg.ReplyTo != "" {
		if err := ValidateSubject(msg.ReplyTo, false); err != nil {
			return broker.PublishResult{}, broker.NewError(broker.ErrValidation, "reply subject: "+err.Error(), false)
		}
	}
	payload, err := base64.StdEncoding.DecodeString(msg.PayloadBase64)
	if err != nil {
		return broker.PublishResult{}, broker.Wrap(broker.ErrValidation, "invalid payload base64", false, err)
	}
	if max := nc.MaxPayload(); max > 0 && int64(len(payload)) > max {
		return broker.PublishResult{}, broker.NewError(broker.ErrLimitExceeded, fmt.Sprintf("payload is %d bytes; server max_payload is %d", len(payload), max), false)
	}
	natsMsg := &nats.Msg{Subject: msg.Subject, Data: payload, Reply: msg.ReplyTo}
	if len(msg.Headers) > 0 {
		natsMsg.Header = nats.Header{}
		for k, vals := range msg.Headers {
			for _, v := range vals {
				natsMsg.Header.Add(k, v)
			}
		}
	}
	if err := nc.PublishMsg(natsMsg); err != nil {
		return broker.PublishResult{}, broker.Wrap(broker.ErrBroker, "publish failed", true, err)
	}
	fctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := nc.FlushWithContext(fctx); err != nil {
		return broker.PublishResult{}, broker.Wrap(broker.ErrBroker, "publish not confirmed by server (flush failed)", true, err)
	}
	return broker.PublishResult{OK: true, MessageID: uuid.NewString()}, nil
}

func (a *Adapter) Request(ctx context.Context, msg broker.OutboundMessage, timeout time.Duration) (*broker.InboundMessage, error) {
	nc, connID, err := a.conn()
	if err != nil {
		return nil, err
	}
	if err := ValidateSubject(msg.Subject, false); err != nil {
		return nil, err
	}
	payload, err := base64.StdEncoding.DecodeString(msg.PayloadBase64)
	if err != nil {
		return nil, broker.Wrap(broker.ErrValidation, "invalid payload base64", false, err)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req := &nats.Msg{Subject: msg.Subject, Data: payload}
	if len(msg.Headers) > 0 {
		req.Header = nats.Header{}
		for k, vals := range msg.Headers {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
	}
	resp, err := nc.RequestMsgWithContext(rctx, req)
	if err != nil {
		switch {
		case errors.Is(err, nats.ErrNoResponders):
			return nil, broker.NewError(broker.ErrBroker, "no responders are subscribed to this subject", false)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
			return nil, broker.NewError(broker.ErrTimeout, "request timed out", true)
		case errors.Is(err, context.Canceled):
			return nil, broker.NewError(broker.ErrCancelled, "request cancelled", false)
		}
		return nil, broker.Wrap(broker.ErrBroker, "request failed", true, err)
	}
	return toInbound(connID, "", resp, len(resp.Data)), nil
}

func (a *Adapter) Subscribe(ctx context.Context, subID string, req broker.SubscribeRequest) (<-chan broker.InboundMessage, error) {
	nc, connID, err := a.conn()
	if err != nil {
		return nil, err
	}
	if err := ValidateSubject(req.Subject, true); err != nil {
		return nil, err
	}
	if req.QueueGroup != "" && strings.ContainsAny(req.QueueGroup, " \t*>") {
		return nil, broker.NewError(broker.ErrValidation, "invalid queue group name", false)
	}

	out := make(chan broker.InboundMessage, 256)
	var (
		closeMu sync.RWMutex
		closed  bool
	)
	handler := func(m *nats.Msg) {
		msg := toInbound(connID, subID, m, len(m.Data))
		closeMu.RLock()
		defer closeMu.RUnlock()
		if closed {
			return
		}
		select {
		case out <- *msg:
		default:
			if req.OnDrop != nil {
				req.OnDrop()
			}
		}
	}

	var sub *nats.Subscription
	if req.QueueGroup != "" {
		sub, err = nc.QueueSubscribe(req.Subject, req.QueueGroup, handler)
	} else {
		sub, err = nc.Subscribe(req.Subject, handler)
	}
	if err != nil {
		close(out)
		return nil, broker.Wrap(broker.ErrBroker, "subscribe failed", true, err)
	}
	fctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	ferr := nc.FlushWithContext(fctx)
	cancel()
	if ferr == nil {
		if err := nc.LastError(); err != nil && strings.Contains(strings.ToLower(err.Error()), "permission") {
			_ = sub.Unsubscribe()
			close(out)
			return nil, broker.Wrap(broker.ErrAuthorization, "subscription not permitted for this subject", false, err)
		}
	}

	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
		closeMu.Lock()
		closed = true
		close(out)
		closeMu.Unlock()
	}()
	return out, nil
}

func toInbound(connID, subID string, m *nats.Msg, size int) *broker.InboundMessage {
	const maxInline = 256 * 1024
	data := m.Data
	truncated := false
	if len(data) > maxInline {
		data = data[:maxInline]
		truncated = true
	}
	headers := broker.MessageHeaders{}
	if m.Header != nil {
		for k, vals := range m.Header {
			headers[k] = append([]string{}, vals...)
		}
	}
	now := time.Now().UTC()
	return &broker.InboundMessage{
		ID:             uuid.NewString(),
		ConnectionID:   connID,
		SubscriptionID: subID,
		BrokerType:     broker.BrokerNATS,
		ReceivedAt:     now,
		Headers:        headers,
		Metadata: broker.MessageMetadata{
			Subject: m.Subject,
			ReplyTo: m.Reply,
		},
		PayloadBase64: base64.StdEncoding.EncodeToString(data),
		SizeBytes:     size,
		Truncated:     truncated,
		AckMode:       "none",
	}
}

func (a *Adapter) jsCtx() (jetstream.JetStream, string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.nc == nil {
		return nil, "", broker.NewError(broker.ErrNetwork, "not connected", true)
	}
	if a.js == nil {
		return nil, "", broker.NewError(broker.ErrUnsupported, "JetStream not available on this connection", false)
	}
	return a.js, a.connID, nil
}

func jsError(err error, what string) error {
	switch {
	case errors.Is(err, jetstream.ErrJetStreamNotEnabled), errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount):
		return broker.NewError(broker.ErrUnsupported, "JetStream is not enabled for this account", false)
	case errors.Is(err, jetstream.ErrStreamNotFound), errors.Is(err, jetstream.ErrConsumerNotFound), errors.Is(err, jetstream.ErrMsgNotFound):
		return broker.NewError(broker.ErrNotFound, what+" not found", false)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return broker.NewError(broker.ErrTimeout, "JetStream request timed out", true)
	case errors.Is(err, context.Canceled):
		return broker.NewError(broker.ErrCancelled, "cancelled", false)
	}
	return broker.Wrap(broker.ErrBroker, "JetStream request failed", true, err)
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

func (a *Adapter) ListResources(ctx context.Context) ([]broker.ResourceDescriptor, error) {
	js, connID, err := a.jsCtx()
	resources := []broker.ResourceDescriptor{}
	if err != nil {
		var be *broker.Error
		if errors.As(err, &be) && be.Code == broker.ErrUnsupported {
			return resources, nil
		}
		return nil, err
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	names := js.StreamNames(ctx)
	for name := range names.Name() {
		resources = append(resources, broker.ResourceDescriptor{
			ID: "stream:" + name, ConnectionID: connID, Kind: broker.ResourceStream, Name: name, Origin: broker.OriginDiscovered,
		})
	}
	if err := names.Err(); err != nil {
		if errors.Is(err, jetstream.ErrJetStreamNotEnabled) || errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount) {
			return resources, nil
		}
		return nil, jsError(err, "streams")
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	return resources, nil
}

func (a *Adapter) ListStreams(ctx context.Context) ([]json.RawMessage, error) {
	js, _, err := a.jsCtx()
	if err != nil {
		return nil, err
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	out := []json.RawMessage{}
	list := js.ListStreams(ctx)
	for info := range list.Info() {
		if b, err := json.Marshal(info); err == nil {
			out = append(out, b)
		}
	}
	if err := list.Err(); err != nil {
		return nil, jsError(err, "streams")
	}
	return out, nil
}

func (a *Adapter) GetStream(ctx context.Context, name string) (json.RawMessage, error) {
	js, _, err := a.jsCtx()
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, broker.NewError(broker.ErrValidation, "stream name is required", false)
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	st, err := js.Stream(ctx, name)
	if err != nil {
		return nil, jsError(err, "stream")
	}
	info, err := st.Info(ctx)
	if err != nil {
		return nil, jsError(err, "stream")
	}
	return json.Marshal(info)
}

func (a *Adapter) ListConsumers(ctx context.Context, stream string) ([]json.RawMessage, error) {
	js, _, err := a.jsCtx()
	if err != nil {
		return nil, err
	}
	if stream == "" {
		return nil, broker.NewError(broker.ErrValidation, "stream name is required", false)
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	out := []json.RawMessage{}
	st, err := js.Stream(ctx, stream)
	if err != nil {
		return nil, jsError(err, "stream")
	}
	list := st.ListConsumers(ctx)
	for info := range list.Info() {
		if b, err := json.Marshal(info); err == nil {
			out = append(out, b)
		}
	}
	if err := list.Err(); err != nil {
		return nil, jsError(err, "stream")
	}
	return out, nil
}

// GetMessage reads a stored message by sequence. This is a direct get: it does
// not create a consumer and does not acknowledge anything.
func (a *Adapter) GetMessage(ctx context.Context, stream string, seq uint64) (*broker.InboundMessage, error) {
	js, connID, err := a.jsCtx()
	if err != nil {
		return nil, err
	}
	if stream == "" || seq == 0 {
		return nil, broker.NewError(broker.ErrValidation, "stream and a sequence ≥ 1 are required", false)
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()
	st, err := js.Stream(ctx, stream)
	if err != nil {
		return nil, jsError(err, "stream")
	}
	raw, err := st.GetMsg(ctx, seq)
	if err != nil {
		return nil, jsError(err, "message")
	}
	msg := &nats.Msg{Subject: raw.Subject, Data: raw.Data, Header: raw.Header}
	in := toInbound(connID, "", msg, len(raw.Data))
	in.Metadata.Stream = stream
	in.Metadata.Sequence = seq
	if !raw.Time.IsZero() {
		t := raw.Time.UTC()
		in.BrokerTimestamp = &t
	}
	return in, nil
}

// ValidateSubject validates NATS publish subjects or subscription patterns.
func ValidateSubject(subject string, allowWildcards bool) error {
	if subject == "" {
		return broker.NewError(broker.ErrValidation, "subject is required", false)
	}
	if strings.Contains(subject, " ") {
		return broker.NewError(broker.ErrValidation, "subject must not contain spaces", false)
	}
	parts := strings.Split(subject, ".")
	for i, p := range parts {
		if p == "" {
			return broker.NewError(broker.ErrValidation, "subject has empty token", false)
		}
		if p == "*" || p == ">" {
			if !allowWildcards {
				return broker.NewError(broker.ErrValidation, "wildcards are not allowed in publish subjects", false)
			}
			if p == ">" && i != len(parts)-1 {
				return broker.NewError(broker.ErrValidation, "> wildcard must be the final token", false)
			}
			continue
		}
		if strings.ContainsAny(p, "*>") {
			return broker.NewError(broker.ErrValidation, "invalid subject token", false)
		}
	}
	return nil
}

// Ensure interfaces are satisfied.
var (
	_ broker.BrokerAdapter     = (*Adapter)(nil)
	_ broker.RequestReply      = (*Adapter)(nil)
	_ broker.ResourceExplorer  = (*Adapter)(nil)
	_ broker.JetStreamExplorer = (*Adapter)(nil)
)
