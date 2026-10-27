package monitoring

import (
	"strings"
	"testing"
)

func TestNormalizeStandaloneFixture(t *testing.T) {
	ep := collectFixture(t, "nats-2.15-standalone")
	if ep.Completeness != 1 {
		t.Fatalf("completeness = %v, status = %+v", ep.Completeness, ep.EndpointStatus)
	}
	if ids := entityIDs(ep, EntityCluster); len(ids) != 0 {
		t.Fatalf("standalone server must not produce cluster entities, got %v", ids)
	}
	srv := mustEntity(t, ep, "server:relay-local")
	if srv.Props["version"] == "" {
		t.Fatal("server version missing")
	}
	mustEntity(t, ep, "stream:$G/ORDERS")
	consumers := entityIDs(ep, EntityConsumer)
	if len(consumers) != 1 || !strings.HasPrefix(consumers[0], "consumer:$G/ORDERS/") {
		t.Fatalf("consumers = %v", consumers)
	}
	if v := metricValue(ep, "stream:$G/ORDERS", "messages"); v == nil || *v != 25 {
		t.Fatalf("ORDERS messages = %v", v)
	}
	conns := entityIDs(ep, EntityConnection)
	if len(conns) != 1 {
		t.Fatalf("connections = %v", conns)
	}
	if c := mustEntity(t, ep, conns[0]); c.DisplayName != "orders-worker" {
		t.Fatalf("connection name = %q", c.DisplayName)
	}
	if js := mustEntity(t, ep, "system:sys").Props["jetstream"]; js != "enabled" {
		t.Fatalf("jetstream prop = %v", js)
	}
	for _, e := range ep.Entities {
		for k, v := range e.Props {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "map[") {
				t.Fatalf("%s.%s is a formatted map: %q", e.ID, k, s)
			}
		}
	}
}

func TestNormalizeClusterFixtureDedupesPooledRoutes(t *testing.T) {
	ep := collectFixture(t, "nats-2.15-cluster-a")
	mustEntity(t, ep, "cluster:C1")
	routes := entityIDs(ep, EntityRoute)
	if len(routes) != 1 {
		t.Fatalf("4 pooled connections to one peer must be one route entity, got %v", routes)
	}
	r := mustEntity(t, ep, routes[0])
	if v := metricValue(ep, r.ID, "pooled_connections"); v == nil || *v != 4 {
		t.Fatalf("pooled_connections = %v", v)
	}
	if len(r.Related) == 0 || r.Related[0] != "server:b" {
		t.Fatalf("route related = %v", r.Related)
	}
	if leafs := entityIDs(ep, EntityLeaf); len(leafs) != 1 {
		t.Fatalf("leafs = %v", leafs)
	}
}

func TestNormalizeJetStreamDisabledIsNotZero(t *testing.T) {
	ep := collectFixture(t, "nats-2.15-cluster-a")
	if js := mustEntity(t, ep, "system:sys").Props["jetstream"]; js != "disabled" {
		t.Fatalf("jetstream prop = %v", js)
	}
	for _, m := range ep.Metrics {
		if strings.HasPrefix(m.Name, "js_") {
			t.Fatalf("JetStream disabled must not emit %s", m.Name)
		}
	}
}

func TestNormalizeGatewayFixture(t *testing.T) {
	ep := collectFixture(t, "nats-2.15-gateway-east")
	gws := entityIDs(ep, EntityGateway)
	if len(gws) != 1 {
		t.Fatalf("gateways = %v", gws)
	}
	if g := mustEntity(t, ep, gws[0]); g.Status != "ok" {
		t.Fatalf("bidirectional gateway status = %q", g.Status)
	}
}

func TestCompletenessIgnoresMissingEndpoint(t *testing.T) {
	st := []EndpointStatus{
		{Endpoint: "/varz", OK: true, Available: true},
		{Endpoint: "/gatewayz", OK: false, Available: false, HTTPCode: 404},
	}
	if c := completeness(st); c != 1 {
		t.Fatalf("expected 1 got %v", c)
	}
}
