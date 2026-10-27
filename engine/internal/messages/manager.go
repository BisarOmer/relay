package messages

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/connections"
)

type EventEmitter func(event string, correlationID string, payload any)

const (
	MaxSubscriptionsPerConnection = 64
	MaxRequestTimeout             = 60 * time.Second
)

type Manager struct {
	conns *connections.Manager
	emit  EventEmitter
	mu    sync.Mutex
	subs  map[string]*activeSub
}

type activeSub struct {
	id       string
	cancel   context.CancelFunc
	req      broker.SubscribeRequest
	paused   atomic.Bool
	msgCount atomic.Int64
	// dropped counts messages discarded while rendering was paused or the
	// delivery buffer was full. They are never acknowledged on the broker.
	dropped atomic.Int64
	stopped atomic.Bool

	mu     sync.Mutex
	status broker.SubscriptionStatus
}

func (as *activeSub) snapshot() broker.SubscriptionStatus {
	as.mu.Lock()
	defer as.mu.Unlock()
	st := as.status
	st.MessageCount = as.msgCount.Load()
	st.DroppedCount = as.dropped.Load()
	return st
}

func (as *activeSub) setStatus(status string, details map[string]string) {
	as.mu.Lock()
	as.status.Status = status
	as.status.Details = details
	as.mu.Unlock()
}

func NewManager(conns *connections.Manager, emit EventEmitter) *Manager {
	return &Manager{conns: conns, emit: emit, subs: make(map[string]*activeSub)}
}

func (m *Manager) Subscribe(parent context.Context, req broker.SubscribeRequest) (*broker.SubscriptionStatus, error) {
	sess, err := m.conns.Session(req.ConnectionID)
	if err != nil {
		return nil, err
	}
	if req.Mode == "" {
		req.Mode = broker.SubscribeObserve
	}
	if req.Mode != broker.SubscribeObserve && req.Mode != broker.SubscribeQueueGrp {
		return nil, broker.NewError(broker.ErrUnsupported, "only observe and queue-group subscriptions are supported", false)
	}
	if req.Mode == broker.SubscribeQueueGrp && req.QueueGroup == "" {
		return nil, broker.NewError(broker.ErrValidation, "queue group name is required", false)
	}
	if req.Mode == broker.SubscribeObserve {
		// Joining a queue group would steal messages from real consumers.
		req.QueueGroup = ""
	}
	m.mu.Lock()
	n := 0
	for _, s := range m.subs {
		if s.req.ConnectionID == req.ConnectionID {
			n++
		}
	}
	m.mu.Unlock()
	if n >= MaxSubscriptionsPerConnection {
		return nil, broker.NewError(broker.ErrLimitExceeded, "too many active subscriptions on this connection", false)
	}

	subID := uuid.NewString()
	ctx, cancel := context.WithCancel(parent)
	as := &activeSub{id: subID, cancel: cancel}
	req.OnDrop = func() { as.dropped.Add(1) }
	as.req = req
	as.status = broker.SubscriptionStatus{
		ID: subID, ConnectionID: req.ConnectionID, Subject: req.Subject, Status: "active", Mode: req.Mode,
	}
	ch, err := sess.Adapter.Subscribe(ctx, subID, req)
	if err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.subs[subID] = as
	m.mu.Unlock()

	go m.pump(as, ch)

	st := as.snapshot()
	m.emit("subscription.status", subID, st)
	return &st, nil
}

func (m *Manager) pump(as *activeSub, ch <-chan broker.InboundMessage) {
	const batchMax = 50
	const flushEvery = 50 * time.Millisecond
	buf := make([]broker.InboundMessage, 0, batchMax)
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()
	lastReported := int64(0)

	flush := func() {
		if d := as.dropped.Load(); d != lastReported {
			lastReported = d
			m.emit("messages.dropped", as.id, map[string]any{
				"subscriptionId": as.id,
				"reason":         dropReason(as),
				"droppedCount":   d,
			})
		}
		if len(buf) == 0 {
			return
		}
		batch := append([]broker.InboundMessage(nil), buf...)
		buf = buf[:0]
		m.emit("messages.inbound", as.id, map[string]any{
			"subscriptionId": as.id,
			"connectionId":   as.req.ConnectionID,
			"messages":       batch,
		})
	}

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				flush()
				m.finish(as)
				return
			}
			if as.paused.Load() {
				as.dropped.Add(1)
				continue
			}
			as.msgCount.Add(1)
			buf = append(buf, msg)
			if len(buf) >= batchMax {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func dropReason(as *activeSub) string {
	if as.paused.Load() {
		return "render_paused"
	}
	return "buffer_full"
}

// finish removes the subscription and emits the final status exactly once.
func (m *Manager) finish(as *activeSub) {
	if !as.stopped.CompareAndSwap(false, true) {
		return
	}
	m.mu.Lock()
	delete(m.subs, as.id)
	m.mu.Unlock()
	as.setStatus("stopped", nil)
	m.emit("subscription.status", as.id, as.snapshot())
}

func (m *Manager) get(id string) (*activeSub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	as, ok := m.subs[id]
	if !ok {
		return nil, broker.NewError(broker.ErrNotFound, "subscription not found", false)
	}
	return as, nil
}

func (m *Manager) Unsubscribe(id string) error {
	as, err := m.get(id)
	if err != nil {
		return err
	}
	as.cancel()
	m.finish(as)
	return nil
}

// StopForConnection ends every subscription on a connection.
func (m *Manager) StopForConnection(connectionID string) {
	m.mu.Lock()
	var list []*activeSub
	for _, s := range m.subs {
		if s.req.ConnectionID == connectionID {
			list = append(list, s)
		}
	}
	m.mu.Unlock()
	for _, s := range list {
		s.cancel()
		m.finish(s)
	}
}

func (m *Manager) List() []broker.SubscriptionStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]broker.SubscriptionStatus, 0, len(m.subs))
	for _, s := range m.subs {
		out = append(out, s.snapshot())
	}
	return out
}

func (m *Manager) Pause(id string) error {
	as, err := m.get(id)
	if err != nil {
		return err
	}
	as.paused.Store(true)
	as.setStatus("paused", map[string]string{
		"note": "Rendering paused. The subscription stays active; messages arriving now are counted as dropped and are not acknowledged.",
	})
	m.emit("subscription.status", id, as.snapshot())
	return nil
}

func (m *Manager) Resume(id string) error {
	as, err := m.get(id)
	if err != nil {
		return err
	}
	as.paused.Store(false)
	as.setStatus("active", nil)
	m.emit("subscription.status", id, as.snapshot())
	return nil
}

func (m *Manager) Publish(ctx context.Context, msg broker.OutboundMessage) (broker.PublishResult, error) {
	sess, err := m.conns.Session(msg.ConnectionID)
	if err != nil {
		return broker.PublishResult{}, err
	}
	if sess.Profile.ReadOnly {
		return broker.PublishResult{}, broker.NewError(broker.ErrAuthorization, "connection is read-only", false)
	}
	return sess.Adapter.Publish(ctx, msg)
}

func (m *Manager) Request(ctx context.Context, msg broker.OutboundMessage, timeoutMs int) (*broker.InboundMessage, error) {
	sess, err := m.conns.Session(msg.ConnectionID)
	if err != nil {
		return nil, err
	}
	// A request publishes to the subject and can trigger side effects.
	if sess.Profile.ReadOnly {
		return nil, broker.NewError(broker.ErrAuthorization, "connection is read-only", false)
	}
	rr, ok := sess.Adapter.(broker.RequestReply)
	if !ok {
		return nil, broker.NewError(broker.ErrUnsupported, "request/reply not supported", false)
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if timeout > MaxRequestTimeout {
		return nil, broker.NewError(broker.ErrValidation, "request timeout must be at most 60 s", false)
	}
	return rr.Request(ctx, msg, timeout)
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	list := make([]*activeSub, 0, len(m.subs))
	for _, s := range m.subs {
		list = append(list, s)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.cancel()
		m.finish(s)
	}
}
