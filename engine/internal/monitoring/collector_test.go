package monitoring

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func statusOf(raw *RawCollection, ep string) EndpointStatus {
	for _, s := range raw.Status {
		if s.Endpoint == ep {
			return s
		}
	}
	return EndpointStatus{}
}

func TestCollectorEndpointOutcomes(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	srv.override("/accountz", http.NotFound)
	srv.override("/subsz", jsonBody(403, `{"error":"forbidden"}`))
	srv.override("/healthz", jsonBody(503, `{"status":"unavailable","error":"JetStream has not established contact with a meta leader"}`))
	srv.override("/leafz", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/", http.StatusFound)
	})
	srv.override("/routez", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>not nats</html>")) })

	raw, err := NewCollector(2*time.Second).Collect(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if s := statusOf(raw, "/accountz"); s.Available || s.OK {
		t.Fatalf("404 must be unavailable: %+v", s)
	}
	if s := statusOf(raw, "/subsz"); !s.PermissionDenied || s.OK {
		t.Fatalf("403 must be permission denied: %+v", s)
	}
	h := statusOf(raw, "/healthz")
	if h.OK || !strings.Contains(h.Error, "meta leader") {
		t.Fatalf("healthz 503 must carry server error text: %+v", h)
	}
	if srv.hitCount("/healthz") != 1 {
		t.Fatalf("definitive healthz 503 must not be retried, hits=%d", srv.hitCount("/healthz"))
	}
	if s := statusOf(raw, "/leafz"); s.OK {
		t.Fatalf("redirect must not be followed: %+v", s)
	}
	if s := statusOf(raw, "/routez"); s.OK || !strings.Contains(s.Error, "not JSON") {
		t.Fatalf("non-JSON must fail: %+v", s)
	}
	if s := statusOf(raw, "/varz"); !s.OK {
		t.Fatalf("varz should succeed: %+v", s)
	}
}

func TestCollectorRetriesTransient5xx(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	fails := 1
	srv.override("/varz", func(w http.ResponseWriter, r *http.Request) {
		if fails > 0 {
			fails--
			w.WriteHeader(502)
			return
		}
		srv.override("/varz", nil)
		jsonBody(200, `{"server_name":"x","server_id":"X"}`)(w, r)
	})
	raw, err := NewCollector(2*time.Second).Collect(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if s := statusOf(raw, "/varz"); !s.OK {
		t.Fatalf("expected success after retry: %+v", s)
	}
}

func TestCollectorRejectsCredentialURL(t *testing.T) {
	_, err := NewCollector(time.Second).Collect(context.Background(), "http://user:pw@127.0.0.1:8222")
	if err == nil || strings.Contains(err.Error(), "pw") {
		t.Fatalf("expected redacted rejection, got %v", err)
	}
}

func TestCollectorUnreachable(t *testing.T) {
	srv := newFixtureServer(t, "nats-2.15-standalone")
	url := srv.URL
	srv.Close()
	raw, err := NewCollector(500*time.Millisecond).Collect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if c := completeness(raw.Status); c != 0 {
		t.Fatalf("completeness = %v", c)
	}
	if s := statusOf(raw, "/varz"); s.Error != "connection refused" {
		t.Fatalf("error = %q", s.Error)
	}
}
