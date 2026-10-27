package monitoring

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Normalize converts raw monitoring responses into topology entities and
// metric samples. It only emits entities and metrics that the server actually
// reported; missing endpoints produce no entities and metrics are recorded as
// unavailable rather than zero.
func Normalize(system SystemConfig, raw *RawCollection) *Epoch {
	now := raw.FetchedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ep := &Epoch{
		ID:             uuid.NewString(),
		SystemID:       system.ID,
		CollectedAt:    now,
		EndpointStatus: raw.Status,
		Completeness:   completeness(raw.Status),
	}
	for _, s := range raw.Status {
		if s.Available && !s.OK {
			ep.ErrorCount++
		}
	}
	b := &builder{system: system, now: now, index: map[string]int{}}

	sysEntity := Entity{
		ID:          "system:" + system.ID,
		Type:        EntitySystem,
		SystemID:    system.ID,
		NativeID:    system.ID,
		DisplayName: system.Name,
		Status:      "ok",
		Props: map[string]any{
			"monitoringBaseUrl": system.MonitoringBaseURL,
			"demo":              system.Demo,
		},
		LastSeenAt: now,
		Freshness:  freshness(ep.Completeness),
	}
	if ep.Completeness == 0 {
		sysEntity.Status = "unreachable"
	} else if ep.Completeness < 0.99 {
		sysEntity.Status = "partial"
	}
	b.add(sysEntity)
	sysID := sysEntity.ID

	serverID := ""
	serverKey := ""
	if varz, ok := decodeMap(raw.Bodies["/varz"]); ok {
		sid := str(varz["server_id"])
		name := str(varz["server_name"])
		if name == "" {
			name = sid
		}
		if name == "" {
			name = "server"
		}
		// server_id is regenerated on every restart; server_name is stable
		// when configured, so it keys the entity and its time series.
		serverKey = name
		serverID = "server:" + serverKey
		parent := sysID
		if cl, ok := varz["cluster"].(map[string]any); ok {
			if cname := str(cl["name"]); cname != "" {
				clusterID := "cluster:" + cname
				b.add(Entity{
					ID: clusterID, Type: EntityCluster, SystemID: system.ID,
					NativeID: cname, DisplayName: cname, ParentID: sysID, Status: "ok",
					Props:      map[string]any{"configured_routes": cl["urls"], "pool_size": cl["pool_size"]},
					LastSeenAt: now, Freshness: "fresh",
				})
				parent = clusterID
			}
		}
		status := "ok"
		healthDetail := ""
		for _, s := range raw.Status {
			if s.Endpoint == "/healthz" && s.Available && !s.OK {
				status = "unhealthy"
				healthDetail = s.Error
			}
		}
		if hz, ok := decodeMap(raw.Bodies["/healthz"]); ok {
			if st := str(hz["status"]); st != "" && st != "ok" {
				status = "unhealthy"
				if e := str(hz["error"]); e != "" {
					healthDetail = e
				}
			}
		}
		props := map[string]any{
			"server_id":       sid,
			"server_name":     name,
			"version":         varz["version"],
			"go":              varz["go"],
			"host":            varz["host"],
			"port":            varz["port"],
			"http_port":       varz["http_port"],
			"start":           varz["start"],
			"uptime":          varz["uptime"],
			"max_payload":     varz["max_payload"],
			"max_connections": varz["max_connections"],
			"cores":           varz["cores"],
		}
		if healthDetail != "" {
			props["health_detail"] = healthDetail
		}
		if js, ok := varz["jetstream"].(map[string]any); ok {
			_, hasCfg := js["config"]
			props["jetstream_enabled"] = hasCfg
		}
		if gw, ok := varz["gateway"].(map[string]any); ok {
			if n := str(gw["name"]); n != "" {
				props["gateway_name"] = n
			}
		}
		b.add(Entity{
			ID: serverID, Type: EntityServer, SystemID: system.ID,
			NativeID: sid, DisplayName: name, ParentID: parent, Status: status,
			Props: props, LastSeenAt: now, Freshness: "fresh",
		})
		b.metrics(serverID, varz, []metricSpec{
			{"connections", "count"}, {"total_connections", "count"}, {"slow_consumers", "count"},
			{"in_msgs", "msgs"}, {"out_msgs", "msgs"}, {"in_bytes", "bytes"}, {"out_bytes", "bytes"},
			{"cpu", "percent"}, {"mem", "bytes"}, {"subscriptions", "count"},
			{"max_connections", "count"}, {"routes", "count"}, {"leafnodes", "count"},
		})
	}

	ownerOf := func() string {
		if serverID != "" {
			return serverID
		}
		return sysID
	}

	if accz, ok := decodeMap(raw.Bodies["/accountz"]); ok {
		sysAcc := str(accz["system_account"])
		if list, ok := accz["accounts"].([]any); ok {
			for _, a := range list {
				name := str(a)
				if name == "" {
					continue
				}
				b.ensureAccount(name, sysID, "accountz", name == sysAcc)
			}
		}
	}

	if connz, ok := decodeMap(raw.Bodies["/connz"]); ok {
		conns, _ := connz["connections"].([]any)
		for _, item := range conns {
			c, _ := item.(map[string]any)
			if c == nil {
				continue
			}
			cid := str(c["cid"])
			eid := "connection:" + idJoin(serverKey, cid)
			acc := firstStr(c["account"], c["acc"])
			name := str(c["name"])
			if name == "" {
				name = "cid " + cid
			}
			var related []string
			if acc != "" {
				related = append(related, b.ensureAccount(acc, sysID, "connz", false))
			}
			b.add(Entity{
				ID: eid, Type: EntityConnection, SystemID: system.ID,
				NativeID: cid, DisplayName: name, ParentID: ownerOf(), Status: "ok",
				Props: map[string]any{
					"cid": cid, "ip": c["ip"], "port": c["port"], "lang": c["lang"], "version": c["version"],
					"account": acc, "user": firstStr(c["authorized_user"], c["user"]),
					"rtt": c["rtt"], "uptime": c["uptime"], "idle": c["idle"], "start": c["start"],
					"pending_bytes": c["pending_bytes"], "subscriptions": c["subscriptions"],
					"in_msgs": c["in_msgs"], "out_msgs": c["out_msgs"],
					"tls_version": c["tls_version"],
				},
				Related:    related,
				LastSeenAt: now, Freshness: "fresh",
			})
			b.metrics(eid, c, []metricSpec{
				{"pending_bytes", "bytes"}, {"in_msgs", "msgs"}, {"out_msgs", "msgs"},
				{"in_bytes", "bytes"}, {"out_bytes", "bytes"}, {"subscriptions", "count"},
			})
			b.metric(eid, "rtt_ms", durationMs(c["rtt"]), "ms")
		}
		if total := numPtr(connz["total"]); total != nil && int(*total) > len(conns) {
			b.setProp(ownerOf(), "connections_listed", len(conns))
			b.setProp(ownerOf(), "connections_total", int(*total))
			b.setProp(ownerOf(), "connections_truncated", true)
		}
	}

	if jsz, ok := decodeMap(raw.Bodies["/jsz"]); ok {
		if disabled, _ := jsz["disabled"].(bool); disabled {
			b.setProp(sysID, "jetstream", "disabled")
		} else {
			b.setProp(sysID, "jetstream", "enabled")
			b.metrics(sysID, jsz, []metricSpec{
				{"streams", "count"}, {"consumers", "count"}, {"messages", "count"}, {"bytes", "bytes"},
				{"memory", "bytes"}, {"storage", "bytes"},
			}, "js_")
			if cfg, ok := jsz["config"].(map[string]any); ok {
				b.metric(sysID, "js_max_storage", numPtr(cfg["max_storage"]), "bytes")
				b.metric(sysID, "js_max_memory", numPtr(cfg["max_memory"]), "bytes")
			}
			accounts, _ := jsz["account_details"].([]any)
			for _, a := range accounts {
				am, _ := a.(map[string]any)
				accName := str(am["name"])
				parent := sysID
				if accName != "" {
					parent = b.ensureAccount(accName, sysID, "jsz", false)
				}
				streams, _ := am["stream_detail"].([]any)
				for _, s := range streams {
					sm, _ := s.(map[string]any)
					sname := str(sm["name"])
					if sname == "" {
						continue
					}
					sid := "stream:" + idJoin(accName, sname)
					cfg, _ := sm["config"].(map[string]any)
					state, _ := sm["state"].(map[string]any)
					b.add(Entity{
						ID: sid, Type: EntityStream, SystemID: system.ID,
						NativeID: sname, DisplayName: sname, ParentID: parent, Status: "ok",
						Props: map[string]any{
							"account": accName, "config": cfg, "state": state,
							"cluster": sm["cluster"], "created": sm["created"], "origin": "discovered",
						},
						LastSeenAt: now, Freshness: "fresh",
					})
					b.metrics(sid, state, []metricSpec{
						{"messages", "count"}, {"bytes", "bytes"}, {"consumer_count", "count"}, {"num_subjects", "count"},
					})
					consumers, _ := sm["consumer_detail"].([]any)
					for _, c := range consumers {
						cm, _ := c.(map[string]any)
						cname := str(cm["name"])
						if cname == "" {
							continue
						}
						cid := "consumer:" + idJoin(accName, sname, cname)
						b.add(Entity{
							ID: cid, Type: EntityConsumer, SystemID: system.ID,
							NativeID: cname, DisplayName: sname + " / " + cname, ParentID: sid, Status: "ok",
							Props: map[string]any{
								"account": accName, "stream": sname, "config": cm["config"],
								"delivered": cm["delivered"], "ack_floor": cm["ack_floor"],
								"num_ack_pending": cm["num_ack_pending"], "num_redelivered": cm["num_redelivered"],
								"num_pending": cm["num_pending"], "num_waiting": cm["num_waiting"],
								"created": cm["created"], "origin": "discovered",
							},
							LastSeenAt: now, Freshness: "fresh",
						})
						b.metrics(cid, cm, []metricSpec{
							{"num_ack_pending", "count"}, {"num_pending", "count"},
							{"num_redelivered", "count"}, {"num_waiting", "count"},
						})
					}
				}
			}
		}
	}

	if routez, ok := decodeMap(raw.Bodies["/routez"]); ok {
		routes, _ := routez["routes"].([]any)
		// Route pooling opens several connections per peer; collapse them so the
		// topology shows one edge per remote server.
		type agg struct {
			remote, remoteID         string
			conns                    int
			inMsgs, outMsgs, pending float64
			rtt                      *float64
			hasIn, hasOut, hasPend   bool
		}
		groups := map[string]*agg{}
		var order []string
		for _, item := range routes {
			r, _ := item.(map[string]any)
			if r == nil {
				continue
			}
			remote := firstStr(r["remote_name"], r["remote_id"])
			if remote == "" {
				remote = "rid " + str(r["rid"])
			}
			g, ok := groups[remote]
			if !ok {
				g = &agg{remote: remote, remoteID: str(r["remote_id"])}
				groups[remote] = g
				order = append(order, remote)
			}
			g.conns++
			if v := numPtr(r["in_msgs"]); v != nil {
				g.inMsgs += *v
				g.hasIn = true
			}
			if v := numPtr(r["out_msgs"]); v != nil {
				g.outMsgs += *v
				g.hasOut = true
			}
			if v := numPtr(r["pending_size"]); v != nil {
				g.hasPend = true
				if *v > g.pending {
					g.pending = *v
				}
			}
			if v := durationMs(r["rtt"]); v != nil && (g.rtt == nil || *v > *g.rtt) {
				g.rtt = v
			}
		}
		for _, remote := range order {
			g := groups[remote]
			eid := "route:" + idJoin(serverKey, remote)
			b.add(Entity{
				ID: eid, Type: EntityRoute, SystemID: system.ID,
				NativeID: g.remoteID, DisplayName: "route → " + remote, ParentID: ownerOf(), Status: "ok",
				Props: map[string]any{
					"remote_name": remote, "remote_id": g.remoteID, "pooled_connections": g.conns,
					"remote_server_entity": "server:" + remote,
				},
				Related:    []string{"server:" + remote},
				LastSeenAt: now, Freshness: "fresh",
			})
			b.metric(eid, "pooled_connections", f64(float64(g.conns)), "count")
			b.metric(eid, "in_msgs", optional(g.hasIn, g.inMsgs), "msgs")
			b.metric(eid, "out_msgs", optional(g.hasOut, g.outMsgs), "msgs")
			b.metric(eid, "pending_bytes", optional(g.hasPend, g.pending), "bytes")
			b.metric(eid, "rtt_ms", g.rtt, "ms")
		}
	}

	if gwz, ok := decodeMap(raw.Bodies["/gatewayz"]); ok {
		local := str(gwz["name"])
		type gw struct {
			outbound, inbound int
			inMsgs, outMsgs   float64
			rtt               *float64
		}
		peers := map[string]*gw{}
		get := func(n string) *gw {
			if peers[n] == nil {
				peers[n] = &gw{}
			}
			return peers[n]
		}
		absorb := func(g *gw, conn map[string]any) {
			if v := numPtr(conn["in_msgs"]); v != nil {
				g.inMsgs += *v
			}
			if v := numPtr(conn["out_msgs"]); v != nil {
				g.outMsgs += *v
			}
			if v := durationMs(conn["rtt"]); v != nil && (g.rtt == nil || *v > *g.rtt) {
				g.rtt = v
			}
		}
		if out, ok := gwz["outbound_gateways"].(map[string]any); ok {
			for name, v := range out {
				m, _ := v.(map[string]any)
				g := get(name)
				g.outbound++
				if conn, ok := m["connection"].(map[string]any); ok {
					absorb(g, conn)
				}
			}
		}
		if in, ok := gwz["inbound_gateways"].(map[string]any); ok {
			for name, v := range in {
				list, _ := v.([]any)
				g := get(name)
				for _, item := range list {
					m, _ := item.(map[string]any)
					g.inbound++
					if conn, ok := m["connection"].(map[string]any); ok {
						absorb(g, conn)
					}
				}
			}
		}
		names := make([]string, 0, len(peers))
		for n := range peers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			g := peers[n]
			eid := "gateway:" + idJoin(serverKey, n)
			status := "ok"
			if g.outbound == 0 || g.inbound == 0 {
				status = "degraded"
			}
			b.add(Entity{
				ID: eid, Type: EntityGateway, SystemID: system.ID,
				NativeID: n, DisplayName: "gateway " + local + " ⇄ " + n, ParentID: ownerOf(), Status: status,
				Props: map[string]any{
					"local_gateway": local, "remote_gateway": n,
					"outbound_connections": g.outbound, "inbound_connections": g.inbound,
				},
				LastSeenAt: now, Freshness: "fresh",
			})
			b.metric(eid, "in_msgs", f64(g.inMsgs), "msgs")
			b.metric(eid, "out_msgs", f64(g.outMsgs), "msgs")
			b.metric(eid, "rtt_ms", g.rtt, "ms")
		}
	}

	if leafz, ok := decodeMap(raw.Bodies["/leafz"]); ok {
		leafs, _ := leafz["leafs"].([]any)
		for _, item := range leafs {
			l, _ := item.(map[string]any)
			if l == nil {
				continue
			}
			name := str(l["name"])
			key := name
			if key == "" {
				key = fmt.Sprintf("%s:%s", str(l["ip"]), str(l["port"]))
			}
			acc := str(l["account"])
			eid := "leaf:" + idJoin(serverKey, key, acc)
			var related []string
			if acc != "" {
				related = append(related, b.ensureAccount(acc, sysID, "leafz", false))
			}
			display := "leaf " + key
			b.add(Entity{
				ID: eid, Type: EntityLeaf, SystemID: system.ID,
				NativeID: str(l["id"]), DisplayName: display, ParentID: ownerOf(), Status: "ok",
				Props: map[string]any{
					"name": name, "account": acc, "ip": l["ip"], "port": l["port"], "rtt": l["rtt"],
					"is_spoke": l["is_spoke"], "compression": l["compression"], "subscriptions": l["subscriptions"],
				},
				Related:    related,
				LastSeenAt: now, Freshness: "fresh",
			})
			b.metrics(eid, l, []metricSpec{{"in_msgs", "msgs"}, {"out_msgs", "msgs"}, {"subscriptions", "count"}})
			b.metric(eid, "rtt_ms", durationMs(l["rtt"]), "ms")
		}
	}

	if subsz, ok := decodeMap(raw.Bodies["/subsz"]); ok {
		b.metric(sysID, "num_subscriptions", numPtr(subsz["num_subscriptions"]), "count")
	}

	ep.Entities = b.entities
	ep.Metrics = b.samples
	return ep
}

type metricSpec struct {
	name, unit string
}

type builder struct {
	system   SystemConfig
	now      time.Time
	entities []Entity
	samples  []MetricSample
	index    map[string]int
}

func (b *builder) add(e Entity) {
	if i, ok := b.index[e.ID]; ok {
		// Same entity reported by multiple endpoints: keep the first, merge props.
		for k, v := range e.Props {
			if _, exists := b.entities[i].Props[k]; !exists {
				b.entities[i].Props[k] = v
			}
		}
		return
	}
	if e.Props == nil {
		e.Props = map[string]any{}
	}
	b.index[e.ID] = len(b.entities)
	b.entities = append(b.entities, e)
}

func (b *builder) setProp(id, key string, v any) {
	if i, ok := b.index[id]; ok {
		b.entities[i].Props[key] = v
	}
}

func (b *builder) ensureAccount(name, parent, source string, system bool) string {
	id := "account:" + name
	if i, ok := b.index[id]; ok {
		sources, _ := b.entities[i].Props["sources"].([]string)
		if !containsStr(sources, source) {
			b.entities[i].Props["sources"] = append(sources, source)
		}
		if system {
			b.entities[i].Props["system_account"] = true
		}
		return id
	}
	b.add(Entity{
		ID: id, Type: EntityAccount, SystemID: b.system.ID,
		NativeID: name, DisplayName: name, ParentID: parent, Status: "ok",
		Props:      map[string]any{"sources": []string{source}, "system_account": system},
		LastSeenAt: b.now, Freshness: "fresh",
	})
	return id
}

func (b *builder) metric(entityID, name string, v *float64, unit string) {
	b.samples = append(b.samples, metric(entityID, name, v, unit))
}

func (b *builder) metrics(entityID string, src map[string]any, specs []metricSpec, prefix ...string) {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	for _, s := range specs {
		var v *float64
		if src != nil {
			v = numPtr(src[s.name])
		}
		b.metric(entityID, p+s.name, v, s.unit)
	}
}

func decodeMap(body json.RawMessage) (map[string]any, bool) {
	if len(body) == 0 {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

func metric(entityID, name string, v *float64, unit string) MetricSample {
	return MetricSample{EntityID: entityID, Name: name, Value: v, Unit: unit, Available: v != nil}
}

func f64(v float64) *float64 { return &v }

func optional(ok bool, v float64) *float64 {
	if !ok {
		return nil
	}
	return &v
}

func durationMs(v any) *float64 {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	d, err := time.ParseDuration(strings.ReplaceAll(s, "\u00b5", "u"))
	if err != nil {
		return nil
	}
	ms := float64(d) / float64(time.Millisecond)
	return &ms
}

func numPtr(v any) *float64 {
	switch t := v.(type) {
	case nil:
		return nil
	case float64:
		return &t
	case float32:
		f := float64(t)
		return &f
	case int:
		f := float64(t)
		return &f
	case int64:
		f := float64(t)
		return &f
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool, int, int64:
		return fmt.Sprint(t)
	default:
		return ""
	}
}

func firstStr(vals ...any) string {
	for _, v := range vals {
		if s := str(v); s != "" {
			return s
		}
	}
	return ""
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func freshness(c float64) string {
	switch {
	case c >= 0.99:
		return "fresh"
	case c > 0:
		return "partial"
	default:
		return "stale"
	}
}

// idJoin builds a stable identifier from parts, substituting "_" for empty parts.
func idJoin(parts ...string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if p == "" {
			p = "_"
		}
		out[i] = p
	}
	return strings.Join(out, "/")
}
