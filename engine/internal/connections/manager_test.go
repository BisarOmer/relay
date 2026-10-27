package connections

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nkeys"
	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/security"
	"github.com/relay-ide/relay/engine/internal/storage"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T, opts *natsserver.Options) *natsserver.Server {
	t.Helper()
	opts.Host = "127.0.0.1"
	opts.NoLog, opts.NoSigs = true, true
	if opts.Port == 0 {
		opts.Port = freePort(t)
	}
	s, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats-server not ready")
	}
	t.Cleanup(s.Shutdown)
	return s
}

type statusLog struct {
	mu     sync.Mutex
	events []broker.ConnectionStatus
}

func (l *statusLog) add(st broker.ConnectionStatus) {
	l.mu.Lock()
	l.events = append(l.events, st)
	l.mu.Unlock()
}

func (l *statusLog) waitFor(t *testing.T, st broker.ConnectionStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, e := range l.events {
			if e == st {
				l.events = nil
				l.mu.Unlock()
				return
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t.Fatalf("status %q not observed; saw %v", st, l.events)
}

func newManager(t *testing.T) (*Manager, *statusLog) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ks, err := security.NewFileKeyStore(dir + "/secrets")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(storage.NewConnectionRepo(db), ks)
	log := &statusLog{}
	m.SetStatusListener(func(_ string, st broker.ConnectionStatus, _ string) { log.add(st) })
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	return m, log
}

func profile(url string) broker.ConnectionProfile {
	return broker.ConnectionProfile{Name: "test", BrokerType: broker.BrokerNATS, Servers: []string{url}, TimeoutMs: 2000}
}

func errCode(err error) broker.ErrorCode {
	var be *broker.Error
	if errors.As(err, &be) {
		return be.Code
	}
	return ""
}

func TestValidateProfile(t *testing.T) {
	bad := []broker.ConnectionProfile{
		{Name: "", Servers: []string{"nats://127.0.0.1:4222"}},
		{Name: "x", Servers: nil},
		{Name: "x", Servers: []string{"nats://alice:hunter2@127.0.0.1:4222"}},
		{Name: "x", Servers: []string{"http://127.0.0.1:4222"}},
		{Name: "x", BrokerType: broker.BrokerKafka, Servers: []string{"nats://127.0.0.1:4222"}},
		{Name: "x", Servers: []string{"nats://127.0.0.1:4222"}, TLS: broker.TLSConfig{Enabled: true, InsecureSkipVerify: true}},
		{Name: "x", Servers: []string{"nats://127.0.0.1:4222"}, TLS: broker.TLSConfig{Enabled: true, CACertPath: "/nonexistent/ca.pem"}},
		{Name: "x", Servers: []string{"nats://127.0.0.1:4222"}, Auth: broker.AuthConfig{Method: broker.AuthUserPass}},
		{Name: "x", Servers: []string{"nats://127.0.0.1:4222"}, TimeoutMs: 10},
	}
	for i, p := range bad {
		if err := ValidateProfile(&p); err == nil {
			t.Fatalf("case %d: expected validation error for %+v", i, p)
		} else if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("validation error leaked secret: %v", err)
		}
	}
	p := broker.ConnectionProfile{Name: " ok ", Servers: []string{"127.0.0.1:4222", " nats://127.0.0.1:4222 ", ""}}
	if err := ValidateProfile(&p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "ok" || len(p.Servers) != 1 || p.Servers[0] != "nats://127.0.0.1:4222" {
		t.Fatalf("normalization failed: %+v", p)
	}
}

func TestConnectPublishSubscribeAndIdempotentConnect(t *testing.T) {
	srv := startServer(t, &natsserver.Options{})
	m, _ := newManager(t)
	p, err := m.Create(UpsertInput{Profile: profile(srv.ClientURL())})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.Connect(ctx, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	sess1, _ := m.Session(p.ID)
	if _, err := m.Connect(ctx, p.ID, ""); err != nil {
		t.Fatal(err)
	}
	sess2, _ := m.Session(p.ID)
	if sess1.Adapter != sess2.Adapter {
		t.Fatal("second connect replaced a healthy session")
	}
	if srv.NumClients() != 1 {
		t.Fatalf("expected 1 client, got %d", srv.NumClients())
	}

	subCtx, cancel := context.WithCancel(ctx)
	ch, err := sess1.Adapter.Subscribe(subCtx, "s1", broker.SubscribeRequest{Subject: "orders.>"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess1.Adapter.Publish(ctx, broker.OutboundMessage{Subject: "orders.created", PayloadBase64: base64.StdEncoding.EncodeToString([]byte(`{"id":1}`))})
	if err != nil || !res.OK {
		t.Fatalf("publish: %v %+v", err, res)
	}
	select {
	case msg := <-ch:
		if msg.Metadata.Subject != "orders.created" {
			t.Fatalf("subject = %q", msg.Metadata.Subject)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not received")
	}
	cancel()

	if err := m.Disconnect(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := m.Status(p.ID); st != broker.StatusDisconnected {
		t.Fatalf("status = %s", st)
	}
	deadline := time.Now().Add(2 * time.Second)
	for srv.NumClients() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.NumClients() != 0 {
		t.Fatal("client still connected after disconnect")
	}
}

func TestReconnectAfterServerRestart(t *testing.T) {
	port := freePort(t)
	srv := startServer(t, &natsserver.Options{Port: port})
	m, log := newManager(t)
	pr := profile(srv.ClientURL())
	pr.Reconnect = broker.ReconnectPolicy{Enabled: true, InitialBackoffMs: 50, MaxBackoffMs: 200}
	p, err := m.Create(UpsertInput{Profile: pr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
		t.Fatal(err)
	}
	log.waitFor(t, broker.StatusConnected, time.Second)

	srv.Shutdown()
	log.waitFor(t, broker.StatusReconnecting, 3*time.Second)
	if st, _ := m.Status(p.ID); st != broker.StatusReconnecting {
		t.Fatalf("status during outage = %s", st)
	}

	startServer(t, &natsserver.Options{Port: port})
	log.waitFor(t, broker.StatusConnected, 5*time.Second)
	if st, _ := m.Status(p.ID); st != broker.StatusConnected {
		t.Fatalf("status after restart = %s", st)
	}
}

func TestDisconnectWithoutReconnectEndsSessionAndSubscriptions(t *testing.T) {
	srv := startServer(t, &natsserver.Options{})
	m, log := newManager(t)
	var stopped sync.WaitGroup
	stopped.Add(1)
	var once sync.Once
	m.SetDisconnectHook(func(string) { once.Do(stopped.Done) })
	p, _ := m.Create(UpsertInput{Profile: profile(srv.ClientURL())})
	if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
		t.Fatal(err)
	}
	srv.Shutdown()
	log.waitFor(t, broker.StatusDisconnected, 3*time.Second)
	done := make(chan struct{})
	go func() { stopped.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect hook not called")
	}
	if _, err := m.Session(p.ID); err == nil {
		t.Fatal("session should be gone")
	}
}

func TestAuthModes(t *testing.T) {
	ukp, _ := nkeys.CreateUser()
	upub, _ := ukp.PublicKey()
	useed, _ := ukp.Seed()

	t.Run("user_pass", func(t *testing.T) {
		srv := startServer(t, &natsserver.Options{Username: "alice", Password: "s3cret-pass"})
		m, _ := newManager(t)
		pr := profile(srv.ClientURL())
		pr.Auth = broker.AuthConfig{Method: broker.AuthUserPass, Username: "alice"}
		p, err := m.Create(UpsertInput{Profile: pr, Secret: "wrong"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Connect(context.Background(), p.ID, "")
		if errCode(err) != broker.ErrAuth {
			t.Fatalf("expected auth error, got %v", err)
		}
		if strings.Contains(err.Error(), "wrong") {
			t.Fatalf("error leaked password: %v", err)
		}
		if _, err := m.Update(UpsertInput{Profile: *p, Secret: "s3cret-pass"}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
			t.Fatalf("connect with stored password: %v", err)
		}
	})

	t.Run("token", func(t *testing.T) {
		srv := startServer(t, &natsserver.Options{Authorization: "tok-123"})
		m, _ := newManager(t)
		pr := profile(srv.ClientURL())
		pr.Auth = broker.AuthConfig{Method: broker.AuthToken}
		p, _ := m.Create(UpsertInput{Profile: pr, Secret: "tok-123"})
		if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nkey in memory", func(t *testing.T) {
		srv := startServer(t, &natsserver.Options{Nkeys: []*natsserver.NkeyUser{{Nkey: upub}}})
		m, _ := newManager(t)
		pr := profile(srv.ClientURL())
		pr.Auth = broker.AuthConfig{Method: broker.AuthNKey}
		if _, err := m.Create(UpsertInput{Profile: pr, Secret: "SUnot-a-seed"}); errCode(err) != broker.ErrValidation {
			t.Fatalf("invalid seed should be rejected, got %v", err)
		}
		p, err := m.Create(UpsertInput{Profile: pr, Secret: string(useed)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
			t.Fatal(err)
		}
	})
}

// A client-supplied secretRef must never cause a stored secret to be sent to
// an arbitrary server.
func TestTestIgnoresClientSuppliedSecretRef(t *testing.T) {
	srv := startServer(t, &natsserver.Options{Username: "alice", Password: "s3cret-pass"})
	m, _ := newManager(t)
	pr := profile(srv.ClientURL())
	pr.Auth = broker.AuthConfig{Method: broker.AuthUserPass, Username: "alice"}
	saved, err := m.Create(UpsertInput{Profile: pr, Secret: "s3cret-pass"})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := m.repo.Get(saved.ID)

	probe := profile(srv.ClientURL())
	probe.Auth = broker.AuthConfig{Method: broker.AuthUserPass, Username: "alice", SecretRef: stored.Auth.SecretRef}
	if _, err := m.Test(context.Background(), "", probe, ""); errCode(err) != broker.ErrAuth {
		t.Fatalf("expected missing-password error, got %v", err)
	}
	if _, err := m.Create(UpsertInput{Profile: probe}); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	for _, p := range list {
		if p.ID != saved.ID && p.Auth.SecretRef != "" {
			t.Fatalf("created profile adopted another connection's secret: %+v", p.Auth)
		}
	}
	// With the saved id, the stored secret is used.
	if h, err := m.Test(context.Background(), saved.ID, pr, ""); err != nil || !h.Healthy {
		t.Fatalf("test with saved id: %+v %v", h, err)
	}
}

func TestReadOnlyCapabilities(t *testing.T) {
	m, _ := newManager(t)
	pr := profile("nats://127.0.0.1:4222")
	pr.ReadOnly = true
	p, _ := m.Create(UpsertInput{Profile: pr})
	caps, err := m.Capabilities(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range caps.Capabilities {
		if c == broker.CapPublish || c == broker.CapRequestReply {
			t.Fatalf("read-only connection advertises %s", c)
		}
	}
}

// Unsubscribing while messages are in flight must not panic (send on closed
// channel) and must not deliver after close.
func TestUnsubscribeUnderLoadDoesNotPanic(t *testing.T) {
	srv := startServer(t, &natsserver.Options{})
	m, _ := newManager(t)
	p, _ := m.Create(UpsertInput{Profile: profile(srv.ClientURL())})
	if _, err := m.Connect(context.Background(), p.ID, ""); err != nil {
		t.Fatal(err)
	}
	sess, _ := m.Session(p.ID)
	for round := 0; round < 20; round++ {
		ctx, cancel := context.WithCancel(context.Background())
		var drops int
		var dmu sync.Mutex
		ch, err := sess.Adapter.Subscribe(ctx, fmt.Sprint(round), broker.SubscribeRequest{Subject: "load.>", OnDrop: func() { dmu.Lock(); drops++; dmu.Unlock() }})
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		go func() {
			payload := base64.StdEncoding.EncodeToString([]byte("x"))
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = sess.Adapter.Publish(context.Background(), broker.OutboundMessage{Subject: "load.a", PayloadBase64: payload})
				}
			}
		}()
		time.Sleep(10 * time.Millisecond)
		cancel()
		for range ch {
		}
		close(stop)
	}
}
