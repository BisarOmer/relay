package monitoring

import (
	"testing"
	"time"
)

func okStatus(endpoints ...string) []EndpointStatus {
	out := make([]EndpointStatus, 0, len(endpoints))
	for _, e := range endpoints {
		out = append(out, EndpointStatus{Endpoint: e, OK: true, Available: true})
	}
	return out
}

func val(v float64) *float64 { return &v }

func TestHealthzFailure(t *testing.T) {
	ep := &Epoch{
		SystemID: "s1", CollectedAt: time.Now().UTC(), Completeness: 0.5,
		EndpointStatus: []EndpointStatus{
			{Endpoint: "/varz", OK: true, Available: true},
			{Endpoint: "/healthz", OK: false, Available: true, HTTPCode: 503, Error: "JetStream not ready"},
		},
	}
	f := healthzFailure().Evaluate(EvalContext{Epoch: ep})
	if len(f) != 1 || f[0].Severity != "" && f[0].Severity != "critical" {
		t.Fatalf("unexpected: %+v", f)
	}
}

func TestSlowConsumersUsesDelta(t *testing.T) {
	now := time.Now().UTC()
	mk := func(v float64, at time.Time) *Epoch {
		return &Epoch{
			SystemID: "s1", CollectedAt: at, Completeness: 1, EndpointStatus: okStatus("/varz"),
			Entities: []Entity{{ID: "server:n1", Type: EntityServer, DisplayName: "n1"}},
			Metrics:  []MetricSample{{EntityID: "server:n1", Name: "slow_consumers", Value: &v, Available: true}},
		}
	}
	c := slowConsumerIncrease()
	// A lifetime total that is not growing is history, not a current problem.
	if f := c.Evaluate(EvalContext{Epoch: mk(5, now), Previous: mk(5, now.Add(-5*time.Second))}); len(f) != 0 {
		t.Fatalf("unchanged counter fired: %+v", f)
	}
	if f := c.Evaluate(EvalContext{Epoch: mk(7, now), Previous: mk(5, now.Add(-5*time.Second))}); len(f) != 1 || f[0].Evidence["delta"] != 2.0 {
		t.Fatalf("increase not reported: %+v", f)
	}
	// Counter reset after restart must not fire.
	if f := c.Evaluate(EvalContext{Epoch: mk(0, now), Previous: mk(5, now.Add(-5*time.Second))}); len(f) != 0 {
		t.Fatalf("reset fired: %+v", f)
	}
	if c.Evaluable(EvalContext{Epoch: mk(1, now)}) {
		t.Fatal("delta check must not be evaluable without a previous snapshot")
	}
}

func TestHighPendingBytesBoundary(t *testing.T) {
	ep := &Epoch{
		SystemID: "s1", CollectedAt: time.Now().UTC(), EndpointStatus: okStatus("/connz"),
		Metrics: []MetricSample{{EntityID: "connection:n1/1", Name: "pending_bytes", Value: val(PendingBytesThreshold - 1), Available: true}},
	}
	if len(highPendingBytes().Evaluate(EvalContext{Epoch: ep})) != 0 {
		t.Fatal("below threshold should not fire")
	}
	ep.Metrics[0].Value = val(PendingBytesThreshold)
	if len(highPendingBytes().Evaluate(EvalContext{Epoch: ep})) != 1 {
		t.Fatal("at threshold should fire")
	}
}

func TestUnavailableMetricSkipped(t *testing.T) {
	ep := &Epoch{
		SystemID: "s1", CollectedAt: time.Now().UTC(), EndpointStatus: okStatus("/connz"),
		Metrics: []MetricSample{{EntityID: "connection:n1/1", Name: "pending_bytes", Available: false}},
	}
	if len(highPendingBytes().Evaluate(EvalContext{Epoch: ep})) != 0 {
		t.Fatal("unavailable must not fabricate findings")
	}
}

func TestChecksNotEvaluableWithoutInputs(t *testing.T) {
	ep := &Epoch{SystemID: "s1", CollectedAt: time.Now().UTC(), EndpointStatus: []EndpointStatus{
		{Endpoint: "/connz", OK: false, Available: true, Error: "timed out"},
	}}
	_, evaluated := EvaluateAll(EvalContext{Epoch: ep})
	for _, id := range []string{"connections.high_pending_bytes", "jetstream.ack_pending_buildup", "cluster.route_lost"} {
		if evaluated[id] {
			t.Fatalf("%s evaluated without its inputs", id)
		}
	}
}

func TestConnectionCapacitySeverity(t *testing.T) {
	mk := func(cur float64) *Epoch {
		return &Epoch{
			SystemID: "s1", CollectedAt: time.Now().UTC(), EndpointStatus: okStatus("/varz"),
			Entities: []Entity{{ID: "server:n1", Type: EntityServer, DisplayName: "n1"}},
			Metrics: []MetricSample{
				{EntityID: "server:n1", Name: "connections", Value: val(cur), Available: true},
				{EntityID: "server:n1", Name: "max_connections", Value: val(100), Available: true},
			},
		}
	}
	if f := connectionCapacity().Evaluate(EvalContext{Epoch: mk(79)}); len(f) != 0 {
		t.Fatal("79% should not fire")
	}
	if f := connectionCapacity().Evaluate(EvalContext{Epoch: mk(80)}); len(f) != 1 || f[0].Severity != "warning" {
		t.Fatalf("80%% should warn: %+v", f)
	}
	if f := connectionCapacity().Evaluate(EvalContext{Epoch: mk(96)}); len(f) != 1 || f[0].Severity != "critical" {
		t.Fatalf("96%% should be critical: %+v", f)
	}
}

func TestRouteLostStaysOpenUntilRouteReturns(t *testing.T) {
	now := time.Now().UTC()
	withRoute := &Epoch{ID: "e1", SystemID: "s1", CollectedAt: now.Add(-10 * time.Second), EndpointStatus: okStatus("/routez"),
		Entities: []Entity{{ID: "route:a/b", Type: EntityRoute, DisplayName: "route → b"}}}
	without := &Epoch{ID: "e2", SystemID: "s1", CollectedAt: now.Add(-5 * time.Second), EndpointStatus: okStatus("/routez")}
	f := routeLost().Evaluate(EvalContext{Epoch: without, Previous: withRoute})
	if len(f) != 1 {
		t.Fatalf("expected route lost: %+v", f)
	}
	f[0].CheckID = "cluster.route_lost"
	still := &Epoch{ID: "e3", SystemID: "s1", CollectedAt: now, EndpointStatus: okStatus("/routez")}
	if g := routeLost().Evaluate(EvalContext{Epoch: still, Previous: without, Open: f}); len(g) != 1 {
		t.Fatalf("loss should persist while route is absent: %+v", g)
	}
	if g := routeLost().Evaluate(EvalContext{Epoch: withRoute, Previous: still, Open: f}); len(g) != 0 {
		t.Fatalf("loss should clear when route returns: %+v", g)
	}
}
