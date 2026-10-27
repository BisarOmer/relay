//go:build integration

package natsadapter

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/relay-ide/relay/engine/internal/broker"
)

func natsURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("RELAY_NATS_URL"); u != "" {
		return u
	}
	return "nats://127.0.0.1:4222"
}

func testCfg(url string) broker.Config {
	return broker.Config{
		Profile: broker.ConnectionProfile{
			ID:         "test",
			BrokerType: broker.BrokerNATS,
			Servers:    []string{url},
			TimeoutMs:  5000,
			Auth:       broker.AuthConfig{Method: broker.AuthNone},
			Reconnect:  broker.ReconnectPolicy{Enabled: false},
		},
	}
}

func TestNATSPublishSubscribe(t *testing.T) {
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := a.Connect(ctx, testCfg(natsURL(t))); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(context.Background())

	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	ch, err := a.Subscribe(subCtx, "sub-1", broker.SubscribeRequest{
		ConnectionID: "test",
		Subject:      "relay.integration",
		Mode:         broker.SubscribeObserve,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte(`{"ok":true}`))
	if _, err := a.Publish(ctx, broker.OutboundMessage{
		ConnectionID:  "test",
		Subject:       "relay.integration",
		PayloadBase64: payload,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case msg := <-ch:
		if msg.Metadata.Subject != "relay.integration" {
			t.Fatalf("subject = %q", msg.Metadata.Subject)
		}
		raw, _ := base64.StdEncoding.DecodeString(msg.PayloadBase64)
		if string(raw) != `{"ok":true}` {
			t.Fatalf("payload = %s", raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestNATSRequestReply(t *testing.T) {
	url := natsURL(t)
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("responder connect: %v", err)
	}
	defer nc.Close()
	_, err = nc.Subscribe("relay.rr", func(m *nats.Msg) {
		_ = m.Respond([]byte(`{"pong":true}`))
	})
	if err != nil {
		t.Fatalf("respond subscribe: %v", err)
	}
	_ = nc.Flush()

	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.Connect(ctx, testCfg(url)); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(context.Background())

	resp, err := a.Request(ctx, broker.OutboundMessage{
		Subject:       "relay.rr",
		PayloadBase64: base64.StdEncoding.EncodeToString([]byte(`{"ping":1}`)),
	}, 3*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(resp.PayloadBase64)
	if string(raw) != `{"pong":true}` {
		t.Fatalf("unexpected reply: %s", raw)
	}
}

func TestJetStreamListStreams(t *testing.T) {
	a := New()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.Connect(ctx, testCfg(natsURL(t))); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer a.Disconnect(context.Background())

	// Listing should succeed even if empty when JetStream is enabled.
	streams, err := a.ListStreams(ctx)
	if err != nil {
		t.Fatalf("list streams: %v", err)
	}
	t.Logf("streams: %d", len(streams))
}
