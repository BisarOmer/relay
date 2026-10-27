//go:build integration

package monitoring

import (
	"context"
	"os"
	"testing"
	"time"
)

// Requires a real nats-server with monitoring enabled. Set
// RELAY_NATS_MONITOR_URL (default http://127.0.0.1:8222).
func TestCollectNormalizePersistFindingsRealServer(t *testing.T) {
	url := os.Getenv("RELAY_NATS_MONITOR_URL")
	if url == "" {
		url = "http://127.0.0.1:8222"
	}
	mgr, _ := newTestManager(t)
	sys, err := mgr.UpsertSystem(SystemConfig{
		Name: "Local", MonitoringBaseURL: url,
		CollectIntervalMs: 60000, RequestTimeoutMs: 3000, RetentionEpochs: 100,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		ep, err := mgr.CollectOnce(ctx, sys.ID)
		if err != nil {
			t.Fatalf("collect: %v", err)
		}
		if ep.Completeness < 0.99 {
			t.Fatalf("expected full collection from a real server, status=%+v", ep.EndpointStatus)
		}
		if len(entityIDs(ep, EntityServer)) != 1 {
			t.Fatal("expected one server entity from /varz")
		}
	}
	ov, err := mgr.Overview(sys.ID)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if ov.Status != "ok" || ov.Servers == nil || ov.Connections == nil || ov.Version == "" {
		t.Fatalf("overview incomplete: %+v", ov)
	}
	pts, err := mgr.Series(sys.ID, ov.ServerEntityID, "in_msgs", 10)
	if err != nil || len(pts) != 2 {
		t.Fatalf("series = %v, %v", pts, err)
	}
	if !pts[0].CollectedAt.Before(pts[1].CollectedAt) {
		t.Fatalf("series must be oldest first: %v then %v", pts[0].CollectedAt, pts[1].CollectedAt)
	}
}
