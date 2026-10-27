package monitoring

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// SimulateEpoch produces a demo snapshot for systems explicitly created in
// demo mode. Values are a deterministic function of wall-clock time so charts
// move, and every entity carries props.demo=true so it can never be mistaken
// for collected data. It never contacts a broker.
func SimulateEpoch(sys SystemConfig) *Epoch {
	now := time.Now().UTC()
	t := float64(now.Unix())
	wave := func(period, amp float64) float64 { return amp * (1 + math.Sin(2*math.Pi*t/period)) / 2 }

	sysID := "system:" + sys.ID
	serverID := "server:demo-n1"
	streamID := "stream:APP/ORDERS"
	consumerID := "consumer:APP/ORDERS/shipping"
	demo := map[string]any{"demo": true}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{"demo": true}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	start := now.Truncate(24 * time.Hour)
	ep := &Epoch{
		ID:           uuid.NewString(),
		SystemID:     sys.ID,
		CollectedAt:  now,
		Completeness: 1,
		EndpointStatus: []EndpointStatus{
			{Endpoint: "/healthz", OK: true, HTTPCode: 200, Available: true},
			{Endpoint: "/varz", OK: true, HTTPCode: 200, Available: true},
			{Endpoint: "/connz", OK: true, HTTPCode: 200, Available: true},
			{Endpoint: "/jsz", OK: true, HTTPCode: 200, Available: true},
			{Endpoint: "/routez", OK: true, HTTPCode: 200, Available: true},
		},
		Entities: []Entity{
			{ID: sysID, Type: EntitySystem, SystemID: sys.ID, DisplayName: sys.Name, Status: "ok",
				Props: with(map[string]any{"jetstream": "enabled"}), LastSeenAt: now, Freshness: "fresh"},
			{ID: "cluster:demo", Type: EntityCluster, SystemID: sys.ID, NativeID: "demo", DisplayName: "demo",
				ParentID: sysID, Status: "ok", Props: demo, LastSeenAt: now, Freshness: "fresh"},
			{ID: serverID, Type: EntityServer, SystemID: sys.ID, NativeID: "demo-n1", DisplayName: "demo-n1",
				ParentID: "cluster:demo", Status: "ok",
				Props: with(map[string]any{"version": "demo", "start": start.Format(time.RFC3339), "server_name": "demo-n1"}), LastSeenAt: now, Freshness: "fresh"},
			{ID: "route:demo-n1/demo-n2", Type: EntityRoute, SystemID: sys.ID, DisplayName: "route → demo-n2",
				ParentID: serverID, Status: "ok", Props: with(map[string]any{"remote_name": "demo-n2", "pooled_connections": 3}),
				Related: []string{"server:demo-n2"}, LastSeenAt: now, Freshness: "fresh"},
			{ID: "account:APP", Type: EntityAccount, SystemID: sys.ID, NativeID: "APP", DisplayName: "APP",
				ParentID: sysID, Status: "ok", Props: demo, LastSeenAt: now, Freshness: "fresh"},
			{ID: "connection:demo-n1/1", Type: EntityConnection, SystemID: sys.ID, NativeID: "1", DisplayName: "orders-svc",
				ParentID: serverID, Status: "ok", Related: []string{"account:APP"},
				Props: with(map[string]any{"account": "APP", "lang": "go"}), LastSeenAt: now, Freshness: "fresh"},
			{ID: "connection:demo-n1/2", Type: EntityConnection, SystemID: sys.ID, NativeID: "2", DisplayName: "shipping-worker",
				ParentID: serverID, Status: "ok", Related: []string{"account:APP"},
				Props: with(map[string]any{"account": "APP", "lang": "rust"}), LastSeenAt: now, Freshness: "fresh"},
			{ID: streamID, Type: EntityStream, SystemID: sys.ID, NativeID: "ORDERS", DisplayName: "ORDERS",
				ParentID: "account:APP", Status: "ok", Props: with(map[string]any{"account": "APP", "origin": "demo"}), LastSeenAt: now, Freshness: "fresh"},
			{ID: consumerID, Type: EntityConsumer, SystemID: sys.ID, NativeID: "shipping",
				DisplayName: "ORDERS / shipping", ParentID: streamID, Status: "ok",
				Props: with(map[string]any{"stream": "ORDERS", "account": "APP", "origin": "demo"}), LastSeenAt: now, Freshness: "fresh"},
		},
	}
	elapsed := now.Sub(start).Seconds()
	m := func(entity, name string, v float64, unit string) MetricSample {
		return MetricSample{EntityID: entity, Name: name, Value: &v, Unit: unit, Available: true}
	}
	ep.Metrics = []MetricSample{
		m(serverID, "connections", 2, "count"),
		m(serverID, "max_connections", 65536, "count"),
		m(serverID, "slow_consumers", 0, "count"),
		m(serverID, "in_msgs", math.Floor(elapsed*120+wave(300, 4000)), "msgs"),
		m(serverID, "out_msgs", math.Floor(elapsed*240+wave(300, 8000)), "msgs"),
		m(serverID, "in_bytes", math.Floor(elapsed*120*512), "bytes"),
		m(serverID, "out_bytes", math.Floor(elapsed*240*512), "bytes"),
		m(serverID, "cpu", 8+wave(120, 10), "percent"),
		m(serverID, "mem", 64e6+wave(600, 16e6), "bytes"),
		m(serverID, "subscriptions", 42, "count"),
		m(sysID, "js_streams", 1, "count"),
		m(sysID, "js_consumers", 1, "count"),
		m(sysID, "js_storage", 5e8, "bytes"),
		m(sysID, "js_max_storage", 1e10, "bytes"),
		m("connection:demo-n1/1", "pending_bytes", 0, "bytes"),
		m("connection:demo-n1/2", "pending_bytes", math.Floor(wave(180, 2048)), "bytes"),
		m(streamID, "messages", math.Floor(1200+wave(900, 300)), "count"),
		m(consumerID, "num_ack_pending", math.Floor(wave(240, 40)), "count"),
		m(consumerID, "num_pending", math.Floor(wave(240, 200)), "count"),
	}
	return ep
}
