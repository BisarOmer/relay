package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
)

func TestConnectionRepoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	migr := filepath.Join("..", "..", "migrations")
	db, err := Open(dir, migr)
	if err != nil {
		// try from module root relative when tests run from package dir
		migr = filepath.Join("..", "..", "..", "migrations")
		db, err = Open(dir, migr)
	}
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	repo := NewConnectionRepo(db)
	now := time.Now().UTC()
	p := broker.ConnectionProfile{
		ID:          "c1",
		Name:        "Local",
		BrokerType:  broker.BrokerNATS,
		Environment: broker.EnvLocal,
		Servers:     []string{"nats://127.0.0.1:4222"},
		Auth:        broker.AuthConfig{Method: broker.AuthNone, EphemeralSecret: "should-not-persist"},
		TLS:         broker.TLSConfig{Enabled: false},
		TimeoutMs:   10000,
		Reconnect:   broker.ReconnectPolicy{Enabled: true, InitialBackoffMs: 500, MaxBackoffMs: 5000},
		CreatedAt:   now,
		UpdatedAt:   now,
		LastStatus:  broker.StatusDisconnected,
	}
	if err := repo.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get("c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Auth.EphemeralSecret != "" {
		t.Fatal("ephemeral secret was persisted")
	}
	if got.Name != "Local" {
		t.Fatalf("name=%s", got.Name)
	}
	list, err := repo.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %#v", err, list)
	}
}
