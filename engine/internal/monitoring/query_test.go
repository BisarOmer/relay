package monitoring

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
)

func seededQueryManager(t *testing.T) (*Manager, string) {
	t.Helper()
	srv := newFixtureServer(t, "nats-2.15-standalone")
	mgr, db := newTestManager(t)
	// A connection profile with credentials in its server list: user SQL must
	// never be able to read it.
	if _, err := db.SQL.Exec(`INSERT INTO connections (id, name, broker_type, environment, servers_json, auth_json, tls_json, reconnect_json, created_at, updated_at)
		VALUES ('c1','prod','nats','production','["nats://alice:hunter2@10.0.0.1:4222"]','{}','{}','{}',datetime('now'),datetime('now'))`); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	sys, err := mgr.UpsertSystem(SystemConfig{Name: "q", MonitoringBaseURL: srv.URL, CollectIntervalMs: 60000, RequestTimeoutMs: 1000, RetentionEpochs: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CollectOnce(context.Background(), sys.ID); err != nil {
		t.Fatal(err)
	}
	return mgr, sys.ID
}

func TestQueryRejectsUnsafeStatements(t *testing.T) {
	mgr, sysID := seededQueryManager(t)
	cases := map[string]string{
		"delete":               "DELETE FROM insights_findings",
		"insert":               "INSERT INTO insights_findings (id) VALUES ('x')",
		"connections table":    "SELECT servers_json FROM connections",
		"connections in cte":   "WITH c AS (SELECT * FROM connections) SELECT * FROM c",
		"connections subquery": "SELECT (SELECT servers_json FROM connections LIMIT 1) AS s FROM insights_systems",
		"sqlite_master":        "SELECT sql FROM sqlite_master",
		"sqlite_schema":        "SELECT * FROM sqlite_schema",
		"pragma":               "PRAGMA table_info(connections)",
		"pragma function":      "SELECT * FROM pragma_table_info('connections')",
		"multi statement":      "SELECT 1; DELETE FROM insights_findings",
		"attach":               "ATTACH DATABASE '/tmp/x.db' AS x",
		"preferences":          "SELECT * FROM preferences",
		"empty":                "   ",
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := mgr.Query(context.Background(), sysID, q, 10, 2000)
			if err == nil {
				t.Fatalf("expected rejection, got %+v", res)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("error leaked secret: %v", err)
			}
		})
	}
}

func TestQueryViewsAndTruncation(t *testing.T) {
	mgr, sysID := seededQueryManager(t)
	res, err := mgr.Query(context.Background(), sysID, "SELECT * FROM v_insights_streams WHERE system_id = :system_id", 10, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowCount != 1 || len(res.Columns) == 0 {
		t.Fatalf("unexpected result %+v", res)
	}
	res, err = mgr.Query(context.Background(), sysID, "SELECT entity_id FROM insights_entities WHERE system_id = :system_id", 2, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowCount != 2 || !res.Truncated {
		t.Fatalf("expected truncation at 2 rows: %+v", res)
	}
}

func TestQueryRunawayIsInterruptedAndEngineStaysResponsive(t *testing.T) {
	mgr, sysID := seededQueryManager(t)
	start := time.Now()
	_, err := mgr.Query(context.Background(), sysID,
		"WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM r) SELECT count(*) FROM r", 10, 300)
	if err == nil {
		t.Fatal("expected timeout")
	}
	var be *broker.Error
	if !errors.As(err, &be) || be.Code != broker.ErrTimeout {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("query was not interrupted promptly: %v", time.Since(start))
	}
	// The writer connection is unaffected and collection still works.
	if _, err := mgr.CollectOnce(context.Background(), sysID); err != nil {
		t.Fatalf("collection blocked after runaway query: %v", err)
	}
	if _, err := mgr.Query(context.Background(), sysID, "SELECT count(*) FROM insights_epochs", 10, 2000); err != nil {
		t.Fatalf("read pool unusable after interrupt: %v", err)
	}
}

func TestQueryCancellation(t *testing.T) {
	mgr, sysID := seededQueryManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, err := mgr.Query(ctx, sysID, "WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM r) SELECT count(*) FROM r", 10, 10000)
	var be *broker.Error
	if !errors.As(err, &be) || be.Code != broker.ErrCancelled {
		t.Fatalf("expected cancelled error, got %v", err)
	}
}

func TestSavedQueriesValidate(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.SaveQuery(SavedQuery{Name: "bad", SQL: "DELETE FROM insights_findings"}); err == nil {
		t.Fatal("saving a mutating query must fail")
	}
	q, err := mgr.SaveQuery(SavedQuery{Name: "streams", SQL: "SELECT * FROM v_insights_streams"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := mgr.ListSavedQueries()
	if len(list) != 1 || list[0].ID != q.ID {
		t.Fatalf("list = %+v", list)
	}
	if err := mgr.DeleteSavedQuery(q.ID); err != nil {
		t.Fatal(err)
	}
}
