package monitoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/relay-ide/relay/engine/internal/storage"
)

// fixtureServer serves captured nats-server monitoring responses from
// testdata/<name>. Individual endpoints can be overridden at runtime.
type fixtureServer struct {
	*httptest.Server
	mu        sync.Mutex
	dir       string
	overrides map[string]http.HandlerFunc
	hits      map[string]int
}

func newFixtureServer(t *testing.T, name string) *fixtureServer {
	t.Helper()
	fs := &fixtureServer{dir: filepath.Join("testdata", name), overrides: map[string]http.HandlerFunc{}, hits: map[string]int{}}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.hits[r.URL.Path]++
		h := fs.overrides[r.URL.Path]
		fs.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join(fs.dir, strings.TrimPrefix(r.URL.Path, "/")+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (f *fixtureServer) override(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h == nil {
		delete(f.overrides, path)
	} else {
		f.overrides[path] = h
	}
}

func (f *fixtureServer) hitCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func jsonBody(code int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

func collectFixture(t *testing.T, name string) *Epoch {
	t.Helper()
	srv := newFixtureServer(t, name)
	raw, err := NewCollector(2*time.Second).Collect(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return Normalize(SystemConfig{ID: "sys", Name: name, MonitoringBaseURL: srv.URL}, raw)
}

func newTestManager(t *testing.T) (*Manager, *storage.DB) {
	t.Helper()
	db, err := storage.Open(t.TempDir(), "")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewManager(NewStore(db), nil), db
}

func entityIDs(ep *Epoch, t EntityType) []string {
	var out []string
	for _, e := range ep.Entities {
		if e.Type == t {
			out = append(out, e.ID)
		}
	}
	return out
}

func mustEntity(t *testing.T, ep *Epoch, id string) *Entity {
	t.Helper()
	e := entityByID(ep, id)
	if e == nil {
		var ids []string
		for _, x := range ep.Entities {
			ids = append(ids, x.ID)
		}
		t.Fatalf("entity %q not found; have %v", id, ids)
	}
	return e
}
