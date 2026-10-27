package app

import (
	"context"
	"encoding/json"

	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/ipc"
	"github.com/relay-ide/relay/engine/internal/monitoring"
)

func decode(req ipc.Request, v any) error {
	if len(req.Params) == 0 {
		return nil
	}
	if err := json.Unmarshal(req.Params, v); err != nil {
		return broker.NewError(broker.ErrValidation, "invalid params", false)
	}
	return nil
}

func required(name, v string) error {
	if v == "" {
		return broker.NewError(broker.ErrValidation, name+" is required", false)
	}
	return nil
}

type systemParams struct {
	SystemID string `json:"systemId"`
}

func (a *App) registerInsightsHandlers() {
	s := a.server

	s.Handle("insights.systems.list", func(ctx context.Context, req ipc.Request) (any, error) {
		list, err := a.insights.ListSystems()
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []monitoring.SystemConfig{}
		}
		for i := range list {
			list[i].Running = a.insights.Running(list[i].ID)
		}
		return map[string]any{"systems": list}, nil
	})

	s.Handle("insights.systems.upsert", func(ctx context.Context, req ipc.Request) (any, error) {
		var c monitoring.SystemConfig
		if err := decode(req, &c); err != nil {
			return nil, err
		}
		return a.insights.UpsertSystem(c)
	})

	s.Handle("insights.systems.delete", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("id", p.ID); err != nil {
			return nil, err
		}
		if err := a.insights.DeleteSystem(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil
	})

	s.Handle("insights.collect", func(ctx context.Context, req ipc.Request) (any, error) {
		var p systemParams
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("systemId", p.SystemID); err != nil {
			return nil, err
		}
		ep, err := a.insights.CollectOnce(ctx, p.SystemID)
		if err != nil {
			return nil, err
		}
		status, lastErr := monitoring.EpochStatus(ep)
		return map[string]any{
			"epochId": ep.ID, "completeness": ep.Completeness, "collectedAt": ep.CollectedAt,
			"entityCount": len(ep.Entities), "errorCount": ep.ErrorCount,
			"status": status, "error": lastErr,
		}, nil
	})

	s.Handle("insights.overview", func(ctx context.Context, req ipc.Request) (any, error) {
		var p systemParams
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		return a.insights.Overview(p.SystemID)
	})

	s.Handle("insights.topology", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID string `json:"systemId"`
			EpochID  string `json:"epochId"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		ep, err := a.insights.Topology(p.SystemID, p.EpochID)
		if err != nil {
			return nil, err
		}
		var at any
		if ep.ID != "" {
			at = ep.CollectedAt
		}
		return map[string]any{
			"epochId": ep.ID, "collectedAt": at, "completeness": ep.Completeness,
			"entities": ep.Entities, "endpointStatus": ep.EndpointStatus,
		}, nil
	})

	s.Handle("insights.entity.get", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID string `json:"systemId"`
			EpochID  string `json:"epochId"`
			EntityID string `json:"entityId"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("entityId", p.EntityID); err != nil {
			return nil, err
		}
		return a.insights.GetEntity(p.SystemID, p.EpochID, p.EntityID)
	})

	s.Handle("insights.epochs.list", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID string `json:"systemId"`
			Limit    int    `json:"limit"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		list, err := a.insights.ListEpochs(p.SystemID, p.Limit)
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []monitoring.Epoch{}
		}
		return map[string]any{"epochs": list}, nil
	})

	s.Handle("insights.metrics.series", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID string `json:"systemId"`
			EntityID string `json:"entityId"`
			Metric   string `json:"metric"`
			Limit    int    `json:"limit"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		pts, err := a.insights.Series(p.SystemID, p.EntityID, p.Metric, p.Limit)
		if err != nil {
			return nil, err
		}
		if pts == nil {
			pts = []monitoring.SeriesPoint{}
		}
		return map[string]any{"points": pts}, nil
	})

	s.Handle("insights.diff", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID  string `json:"systemId"`
			FromEpoch string `json:"fromEpoch"`
			ToEpoch   string `json:"toEpoch"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		return a.insights.Diff(p.SystemID, p.FromEpoch, p.ToEpoch)
	})

	s.Handle("insights.findings.list", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID        string `json:"systemId"`
			IncludeResolved bool   `json:"includeResolved"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		list, err := a.insights.ListFindings(p.SystemID, p.IncludeResolved)
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []monitoring.Finding{}
		}
		return map[string]any{"findings": list}, nil
	})

	s.Handle("insights.findings.events", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("id", p.ID); err != nil {
			return nil, err
		}
		ev, err := a.insights.FindingEvents(p.ID)
		if err != nil {
			return nil, err
		}
		if ev == nil {
			ev = []monitoring.FindingEvent{}
		}
		return map[string]any{"events": ev}, nil
	})

	s.Handle("insights.findings.ack", func(ctx context.Context, req ipc.Request) (any, error) {
		p := struct {
			ID           string `json:"id"`
			Acknowledged *bool  `json:"acknowledged"`
		}{}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("id", p.ID); err != nil {
			return nil, err
		}
		ack := p.Acknowledged == nil || *p.Acknowledged
		if err := a.insights.SetAcknowledged(p.ID, ack); err != nil {
			return nil, err
		}
		return map[string]any{"acknowledged": ack}, nil
	})

	s.Handle("insights.findings.resolve", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("id", p.ID); err != nil {
			return nil, err
		}
		if err := a.insights.ResolveFinding(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"resolved": true}, nil
	})

	s.Handle("insights.query", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			SystemID  string `json:"systemId"`
			SQL       string `json:"sql"`
			Limit     int    `json:"limit"`
			TimeoutMs int    `json:"timeoutMs"`
			// QueryID lets the UI cancel this query via insights.query.cancel.
			QueryID string `json:"queryId"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if p.QueryID != "" {
			qctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if _, loaded := a.queries.LoadOrStore(p.QueryID, cancel); loaded {
				return nil, broker.NewError(broker.ErrValidation, "queryId already in use", false)
			}
			defer a.queries.Delete(p.QueryID)
			ctx = qctx
		}
		return a.insights.Query(ctx, p.SystemID, p.SQL, p.Limit, p.TimeoutMs)
	})

	s.Handle("insights.query.cancel", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			QueryID string `json:"queryId"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if v, ok := a.queries.Load(p.QueryID); ok {
			v.(context.CancelFunc)()
			return map[string]any{"cancelled": true}, nil
		}
		return map[string]any{"cancelled": false}, nil
	})

	s.Handle("insights.schema", func(ctx context.Context, req ipc.Request) (any, error) {
		objs, err := monitoring.QuerySchema(ctx, a.db.SQL)
		if err != nil {
			return nil, err
		}
		return map[string]any{"objects": objs}, nil
	})

	s.Handle("insights.queries.list", func(ctx context.Context, req ipc.Request) (any, error) {
		list, err := a.insights.ListSavedQueries()
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []monitoring.SavedQuery{}
		}
		return map[string]any{"queries": list}, nil
	})

	s.Handle("insights.queries.save", func(ctx context.Context, req ipc.Request) (any, error) {
		var q monitoring.SavedQuery
		if err := decode(req, &q); err != nil {
			return nil, err
		}
		return a.insights.SaveQuery(q)
	})

	s.Handle("insights.queries.delete", func(ctx context.Context, req ipc.Request) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(req, &p); err != nil {
			return nil, err
		}
		if err := required("id", p.ID); err != nil {
			return nil, err
		}
		if err := a.insights.DeleteSavedQuery(p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil
	})
}
