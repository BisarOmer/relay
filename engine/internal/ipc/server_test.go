package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func responses(out string) map[string]Response {
	got := map[string]Response{}
	for _, line := range strings.Split(out, "\n") {
		var r Response
		if json.Unmarshal([]byte(line), &r) == nil && r.ID != "" {
			got[r.ID] = r
		}
	}
	return got
}

func waitResponse(t *testing.T, out *syncBuffer, id string) Response {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r, ok := responses(out.String())[id]; ok {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no response for %s: %s", id, out.String())
	return Response{}
}

func newTestServer(t *testing.T) (*Server, *io.PipeWriter, *syncBuffer, chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	srv := NewServer(inR, out, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done := make(chan error, 1)
	t.Cleanup(func() { _ = inW.Close() })
	return srv, inW, out, done
}

func TestServerRequestResponse(t *testing.T) {
	srv, in, out, done := newTestServer(t)
	srv.Handle("ping", func(ctx context.Context, req Request) (any, error) {
		return map[string]any{"pong": true}, nil
	})
	go func() { done <- srv.Run(context.Background()) }()
	_, _ = in.Write([]byte(`{"type":"request","id":"1","method":"ping","params":{}}` + "\n"))
	if r := waitResponse(t, out, "1"); !r.OK {
		t.Fatalf("not ok: %+v", r.Error)
	}
}

func TestHandlerPanicReturnsError(t *testing.T) {
	srv, in, out, done := newTestServer(t)
	srv.Handle("boom", func(ctx context.Context, req Request) (any, error) { panic("kaboom") })
	srv.Handle("ping", func(ctx context.Context, req Request) (any, error) { return "pong", nil })
	go func() { done <- srv.Run(context.Background()) }()
	_, _ = in.Write([]byte(`{"type":"request","id":"a","method":"boom"}` + "\n"))
	if r := waitResponse(t, out, "a"); r.OK || r.Error == nil {
		t.Fatalf("expected error response: %+v", r)
	}
	_, _ = in.Write([]byte(`{"type":"request","id":"b","method":"ping"}` + "\n"))
	if r := waitResponse(t, out, "b"); !r.OK {
		t.Fatal("server unusable after handler panic")
	}
}

func TestInflightRequestsCompleteOnEOF(t *testing.T) {
	srv, in, out, done := newTestServer(t)
	srv.Handle("slow", func(ctx context.Context, req Request) (any, error) {
		time.Sleep(150 * time.Millisecond)
		return "done", nil
	})
	go func() { done <- srv.Run(context.Background()) }()
	_, _ = in.Write([]byte(`{"type":"request","id":"s","method":"slow"}` + "\n"))
	time.Sleep(20 * time.Millisecond)
	_ = in.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
	if r, ok := responses(out.String())["s"]; !ok || !r.OK {
		t.Fatalf("in-flight response lost on EOF: %s", out.String())
	}
}

func TestErrorCausesAreRedacted(t *testing.T) {
	srv, in, out, done := newTestServer(t)
	srv.Handle("leak", func(ctx context.Context, req Request) (any, error) {
		return nil, errors.New("dial nats://alice:hunter2@10.0.0.1:4222 failed; token=abc123")
	})
	go func() { done <- srv.Run(context.Background()) }()
	_, _ = in.Write([]byte(`{"type":"request","id":"x","method":"leak"}` + "\n"))
	r := waitResponse(t, out, "x")
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "abc123") {
		t.Fatalf("secret leaked in error: %s", b)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv, in, out, done := newTestServer(t)
	go func() { done <- srv.Run(context.Background()) }()
	_, _ = in.Write([]byte(`{"type":"request","id":"u","method":"nope"}` + "\n"))
	if r := waitResponse(t, out, "u"); r.OK {
		t.Fatal("unknown method should fail")
	}
}
