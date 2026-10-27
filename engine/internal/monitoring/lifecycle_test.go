package monitoring

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func connzWithPending(t *testing.T, pending float64) http.HandlerFunc {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "nats-2.15-standalone", "connz.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for _, c := range m["connections"].([]any) {
		c.(map[string]any)["pending_bytes"] = pending
	}
	out, _ := json.Marshal(m)
	return jsonBody(200, string(out))
}

func findingFor(list []Finding, checkID string) *Finding {
	for i := range list {
		if list[i].CheckID == checkID {
			return &list[i]
		}
	}
	return nil
}

func TestFindingLifecycleAcrossPartialAndUnreachableCollections(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	mgr, _ := newTestManager(t)
	ctx := context.Background()
	sys, err := mgr.UpsertSystem(SystemConfig{Name: "fixture", MonitoringBaseURL: srv.URL, CollectIntervalMs: 60000, RequestTimeoutMs: 1000, RetentionEpochs: 100})
	if err != nil {
		t.Fatal(err)
	}
	collect := func() {
		t.Helper()
		if _, err := mgr.CollectOnce(ctx, sys.ID); err != nil {
			t.Fatal(err)
		}
	}
	open := func() []Finding {
		t.Helper()
		l, err := mgr.ListFindings(sys.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}

	// 1. Pending bytes above threshold opens a finding.
	srv.override("/connz", connzWithPending(t, 2<<20))
	collect()
	f := findingFor(open(), "connections.high_pending_bytes")
	if f == nil || f.Status == FindingResolved {
		t.Fatalf("expected open pending finding, got %+v", open())
	}
	id := f.ID

	// 2. /connz fails: the check cannot be evaluated, so the finding must stay
	// open rather than be resolved for lack of evidence.
	srv.override("/connz", jsonBody(500, `{}`))
	collect()
	cur := open()
	if findingFor(cur, "connections.high_pending_bytes") == nil {
		t.Fatal("finding was resolved although /connz was not collected")
	}
	if findingFor(cur, "infrastructure.partial_collection") == nil {
		t.Fatal("expected partial collection finding")
	}
	if s, _ := mgr.GetSystem(sys.ID); s.LastStatus != "partial" {
		t.Fatalf("status = %q", s.LastStatus)
	}

	// 3. Server unreachable: still not resolved, unreachable finding opens.
	srv.override("/connz", nil)
	url := srv.URL
	srv.Close()
	collect()
	cur = open()
	if findingFor(cur, "connections.high_pending_bytes") == nil {
		t.Fatal("finding resolved while server was unreachable")
	}
	if findingFor(cur, "availability.monitoring_unreachable") == nil {
		t.Fatal("expected unreachable finding")
	}
	ov, err := mgr.Overview(sys.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Status != "unreachable" || ov.Connections != nil || ov.Servers != nil {
		t.Fatalf("unreachable overview must not report values: status=%s conns=%v servers=%v", ov.Status, ov.Connections, ov.Servers)
	}

	// 4. Recovery with healthy data resolves the finding and the outage.
	srv2 := newFixtureServer(t, "nats-2.15-standalone")
	sys.MonitoringBaseURL = srv2.URL
	if _, err := mgr.UpsertSystem(*sys); err != nil {
		t.Fatal(err)
	}
	_ = url
	collect()
	cur = open()
	for _, c := range []string{"connections.high_pending_bytes", "availability.monitoring_unreachable", "infrastructure.partial_collection"} {
		if findingFor(cur, c) != nil {
			t.Fatalf("%s should be resolved", c)
		}
	}
	all, _ := mgr.ListFindings(sys.ID, true)
	if r := findingFor(all, "connections.high_pending_bytes"); r == nil || r.Status != "resolved" || r.ResolvedAt == nil {
		t.Fatalf("expected resolved finding with resolvedAt, got %+v", r)
	}

	// 5. Recurrence reopens the same finding instead of creating a new one.
	srv2.override("/connz", connzWithPending(t, 3<<20))
	collect()
	f = findingFor(open(), "connections.high_pending_bytes")
	if f == nil || f.ID != id {
		t.Fatalf("expected reopened finding %s, got %+v", id, f)
	}
	events, _ := mgr.FindingEvents(id)
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.EventType] = true
	}
	for _, k := range []string{"opened", "resolved", "reopened"} {
		if !kinds[k] {
			t.Fatalf("missing %q event; events=%+v", k, events)
		}
	}

	// 6. Acknowledge, un-acknowledge and manual resolve.
	if err := mgr.SetAcknowledged(id, true); err != nil {
		t.Fatal(err)
	}
	if f = findingFor(open(), "connections.high_pending_bytes"); f.AcknowledgedAt == nil {
		t.Fatal("ack not persisted")
	}
	if err := mgr.SetAcknowledged(id, false); err != nil {
		t.Fatal(err)
	}
	if f = findingFor(open(), "connections.high_pending_bytes"); f.AcknowledgedAt != nil {
		t.Fatal("unack not persisted")
	}
	if err := mgr.ResolveFinding(id); err != nil {
		t.Fatal(err)
	}
	if findingFor(open(), "connections.high_pending_bytes") != nil {
		t.Fatal("manual resolve did not close the finding")
	}
}

func TestCollectOnceIsSingleFlight(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	mgr, _ := newTestManager(t)
	sys, err := mgr.UpsertSystem(SystemConfig{Name: "f", MonitoringBaseURL: srv.URL, CollectIntervalMs: 60000, RequestTimeoutMs: 1000, RetentionEpochs: 100})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, err := mgr.CollectOnce(context.Background(), sys.ID); done <- err }()
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	eps, _ := mgr.ListEpochs(sys.ID, 10)
	if len(eps) != 4 {
		t.Fatalf("epochs = %d", len(eps))
	}
	f, _ := mgr.ListFindings(sys.ID, true)
	seen := map[string]bool{}
	for _, x := range f {
		k := x.CheckID + "|" + x.EntityID
		if seen[k] {
			t.Fatalf("duplicate finding %s", k)
		}
		seen[k] = true
	}
}

func TestUpsertSystemValidation(t *testing.T) {
	mgr, _ := newTestManager(t)
	bad := []SystemConfig{
		{Name: "", MonitoringBaseURL: "http://127.0.0.1:8222"},
		{Name: "x", MonitoringBaseURL: "nats://127.0.0.1:4222"},
		{Name: "x", MonitoringBaseURL: "http://u:p@127.0.0.1:8222"},
		{Name: "x", MonitoringBaseURL: "http://127.0.0.1:8222", CollectIntervalMs: 10},
		{Name: "x", MonitoringBaseURL: "http://127.0.0.1:8222", CollectIntervalMs: 2000, RequestTimeoutMs: 5000},
	}
	for _, c := range bad {
		if _, err := mgr.UpsertSystem(c); err == nil {
			t.Fatalf("expected validation error for %+v", c)
		}
	}
	s, err := mgr.UpsertSystem(SystemConfig{Name: " ok ", MonitoringBaseURL: "http://127.0.0.1:8222/"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "ok" || s.MonitoringBaseURL != "http://127.0.0.1:8222" || s.CollectIntervalMs != 5000 {
		t.Fatalf("normalization failed: %+v", s)
	}
}

func TestUpsertDoesNotClobberCollectionStatus(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	mgr, _ := newTestManager(t)
	sys, _ := mgr.UpsertSystem(SystemConfig{Name: "f", MonitoringBaseURL: srv.URL, CollectIntervalMs: 60000, RequestTimeoutMs: 1000, RetentionEpochs: 100})
	if _, err := mgr.CollectOnce(context.Background(), sys.ID); err != nil {
		t.Fatal(err)
	}
	stale := *sys // as a UI would send it back, with stale status fields
	stale.Name = "renamed"
	if _, err := mgr.UpsertSystem(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := mgr.GetSystem(sys.ID)
	if got.Name != "renamed" || got.LastEpochID == "" || got.LastStatus != "ok" {
		t.Fatalf("collection state clobbered: %+v", got)
	}
}
