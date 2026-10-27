package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/logging"
)

type Handler func(ctx context.Context, req Request) (any, error)

type Server struct {
	in       io.Reader
	out      io.Writer
	log      *slog.Logger
	handlers map[string]Handler
	mu       sync.RWMutex
	writeMu  sync.Mutex
	cancel   map[string]context.CancelFunc
	closed   atomic.Bool
	inflight sync.WaitGroup
}

func NewServer(in io.Reader, out io.Writer, log *slog.Logger) *Server {
	return &Server{
		in:       in,
		out:      out,
		log:      log,
		handlers: make(map[string]Handler),
		cancel:   make(map[string]context.CancelFunc),
	}
}

func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) Emit(ev Event) error {
	if s.closed.Load() {
		return fmt.Errorf("server closed")
	}
	return s.writeJSON(ev)
}

func (s *Server) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxFrameBytes {
		return fmt.Errorf("frame too large: %d", len(b))
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.out.Write(append(b, '\n'))
	return err
}

func (s *Server) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cancel[id]; ok {
		c()
		delete(s.cancel, id)
		return true
	}
	return false
}

func (s *Server) registerCancel(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel[id] = cancel
}

func (s *Server) unregisterCancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancel, id)
}

func (s *Server) Close() {
	s.closed.Store(true)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.cancel {
		c()
		delete(s.cancel, id)
	}
}

func (s *Server) Run(ctx context.Context) error {
	reader := bufio.NewReaderSize(s.in, 1024*1024)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// Let in-flight requests write their responses before returning.
			s.inflight.Wait()
			if err == io.EOF {
				return nil
			}
			return err
		}
		if len(line) == 0 || (len(line) == 1 && line[0] == '\n') {
			continue
		}
		if len(line) > MaxFrameBytes {
			s.log.Error("frame too large", "size", len(line))
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.log.Error("invalid json frame", "err", err)
			continue
		}
		if req.Method == "" || req.ID == "" {
			continue
		}
		s.inflight.Add(1)
		go func() {
			defer s.inflight.Done()
			s.dispatch(ctx, req)
		}()
	}
}

func (s *Server) dispatch(parent context.Context, req Request) {
	timeout := 30 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	s.registerCancel(req.ID, cancel)
	defer func() {
		cancel()
		s.unregisterCancel(req.ID)
	}()

	s.mu.RLock()
	h, ok := s.handlers[req.Method]
	s.mu.RUnlock()
	if !ok {
		_ = s.writeJSON(NewErrorResponse(req.ID, "not_found", "unknown method: "+req.Method, false))
		return
	}

	defer func() {
		if r := recover(); r != nil {
			s.log.Error("handler panic", "method", req.Method, "panic", fmt.Sprint(r))
			_ = s.writeJSON(NewErrorResponse(req.ID, string(broker.ErrInternal), "internal error in "+req.Method, false))
		}
	}()
	result, err := h(ctx, req)
	if err != nil {
		code, msg, retry := mapError(err)
		_ = s.writeJSON(NewErrorResponse(req.ID, code, msg, retry))
		return
	}
	_ = s.writeJSON(NewResponse(req.ID, result))
}

// mapError converts handler errors to wire codes. The underlying cause is
// appended (redacted) so failures are diagnosable without leaking secrets.
func mapError(err error) (code, message string, retryable bool) {
	var be *broker.Error
	if errors.As(err, &be) {
		msg := be.Message
		if be.Cause != nil {
			cause := be.Cause.Error()
			if errors.Is(be.Cause, context.DeadlineExceeded) {
				cause = "deadline exceeded"
			}
			if cause != "" && !strings.Contains(msg, cause) {
				msg += ": " + cause
			}
		}
		return string(be.Code), logging.Redact(msg), be.Retryable
	}
	if errors.Is(err, context.Canceled) {
		return string(broker.ErrCancelled), "cancelled", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return string(broker.ErrTimeout), "timeout", true
	}
	return string(broker.ErrInternal), logging.Redact(err.Error()), false
}
