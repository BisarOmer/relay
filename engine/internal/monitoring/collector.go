package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/relay-ide/relay/engine/internal/security"
)

// Endpoints we attempt. Missing ones are marked unavailable, not zeroed.
var defaultEndpoints = []string{
	"/healthz",
	"/varz",
	"/connz",
	"/jsz",
	"/routez",
	"/gatewayz",
	"/leafz",
	"/subsz",
	"/accountz",
}

// endpointQuery holds the query parameters each endpoint needs to return the
// detail the normalizer consumes.
var endpointQuery = map[string]string{
	"/healthz": "js-enabled-only=false",
	"/connz":   "limit=1024&offset=0&auth=true&subs=false",
	"/jsz":     "accounts=true&streams=true&consumers=true&config=true",
}

const maxBodyBytes = 16 << 20

type RawCollection struct {
	Bodies    map[string]json.RawMessage
	Status    []EndpointStatus
	FetchedAt time.Time
}

type Collector struct {
	client *http.Client
}

func NewCollector(timeout time.Duration) *Collector {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Collector{
		client: &http.Client{
			Timeout: timeout,
			// Monitoring endpoints never redirect; following redirects could
			// silently point collection at a different host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Collect fetches every endpoint concurrently (bounded). It never returns
// fabricated bodies: failures are reported per endpoint in Status.
func (c *Collector) Collect(ctx context.Context, baseURL string) (*RawCollection, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if err := security.ValidateEndpointURL(baseURL, "http", "https"); err != nil {
		return nil, fmt.Errorf("invalid monitoring URL: %w", err)
	}
	out := &RawCollection{
		Bodies:    make(map[string]json.RawMessage),
		FetchedAt: time.Now().UTC(),
	}
	type result struct {
		status EndpointStatus
		body   json.RawMessage
	}
	results := make([]result, len(defaultEndpoints))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, ep := range defaultEndpoints {
		wg.Add(1)
		go func(i int, ep string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				results[i] = result{status: EndpointStatus{Endpoint: ep, Available: true, Error: ctx.Err().Error()}}
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()
			st, body := c.fetch(ctx, baseURL, ep)
			results[i] = result{status: st, body: body}
		}(i, ep)
	}
	wg.Wait()
	for i, r := range results {
		out.Status = append(out.Status, r.status)
		if r.status.OK && len(r.body) > 0 {
			out.Bodies[defaultEndpoints[i]] = r.body
		} else if defaultEndpoints[i] == "/healthz" && len(r.body) > 0 {
			// An unhealthy /healthz still carries the server's explanation.
			out.Bodies["/healthz"] = r.body
		}
	}
	return out, nil
}

func (c *Collector) fetch(ctx context.Context, base, ep string) (EndpointStatus, json.RawMessage) {
	start := time.Now()
	st := EndpointStatus{Endpoint: ep, Available: true}
	u := base + ep
	if q, ok := endpointQuery[ep]; ok {
		u += "?" + q
	}
	var lastErr error
attempts:
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100*(1<<attempt))*time.Millisecond + time.Duration(rand.Intn(50))*time.Millisecond
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				break attempts
			case <-time.After(backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			break
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "relay-insights")
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			if !retryable(err) {
				break
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		_ = resp.Body.Close()
		st.HTTPCode = resp.StatusCode
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > maxBodyBytes {
			lastErr = fmt.Errorf("response larger than %d MiB", maxBodyBytes>>20)
			break attempts
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			st.Available = false
			st.Error = "endpoint not exposed by this server"
			st.DurationMs = time.Since(start).Milliseconds()
			return st, nil
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			st.PermissionDenied = true
			st.Error = fmt.Sprintf("permission denied (HTTP %d)", resp.StatusCode)
			st.DurationMs = time.Since(start).Milliseconds()
			return st, nil
		case ep == "/healthz" && resp.StatusCode == http.StatusServiceUnavailable && json.Valid(body):
			// Definitive "unhealthy" answer; do not retry.
			st.Error = healthzError(body, resp.StatusCode)
			st.DurationMs = time.Since(start).Milliseconds()
			return st, json.RawMessage(body)
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		case resp.StatusCode < 200 || resp.StatusCode >= 300:
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			break attempts
		case !json.Valid(body):
			lastErr = fmt.Errorf("response is not JSON (is this the NATS monitoring port?)")
			break attempts
		}
		st.OK = true
		st.DurationMs = time.Since(start).Milliseconds()
		return st, json.RawMessage(body)
	}
	st.DurationMs = time.Since(start).Milliseconds()
	if lastErr == nil {
		lastErr = errors.New("request failed")
	}
	st.Error = describeFetchError(lastErr)
	return st, nil
}

func healthzError(body []byte, code int) string {
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if e, ok := m["error"].(string); ok && e != "" {
			return e
		}
		if s, ok := m["status"].(string); ok && s != "" {
			return fmt.Sprintf("status %q (HTTP %d)", s, code)
		}
	}
	return fmt.Sprintf("HTTP %d", code)
}

func retryable(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return false
	}
	return !errors.Is(err, context.Canceled)
}

func describeFetchError(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return "host not found: " + dnsErr.Name
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "Client.Timeout"):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case strings.Contains(err.Error(), "connection refused"):
		return "connection refused"
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.HasPrefix(msg, "Get ") {
		msg = msg[i+2:]
	}
	return msg
}

func completeness(status []EndpointStatus) float64 {
	var ok, attempted int
	for _, s := range status {
		if !s.Available {
			continue
		}
		attempted++
		if s.OK {
			ok++
		}
	}
	if attempted == 0 {
		return 0
	}
	return float64(ok) / float64(attempted)
}
