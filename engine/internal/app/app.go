package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/connections"
	"github.com/relay-ide/relay/engine/internal/ipc"
	"github.com/relay-ide/relay/engine/internal/logging"
	"github.com/relay-ide/relay/engine/internal/messages"
	"github.com/relay-ide/relay/engine/internal/monitoring"
	"github.com/relay-ide/relay/engine/internal/security"
	"github.com/relay-ide/relay/engine/internal/storage"
)

const EngineVersion = "0.2.0"

type App struct {
	log      *slog.Logger
	server   *ipc.Server
	db       *storage.DB
	conns    *connections.Manager
	msgs     *messages.Manager
	insights *monitoring.Manager
	secrets  security.KeyStore
	prefs    *storage.PreferencesRepo
	dataDir  string
	migrDir  string
	keystore string
	shutdown sync.Once
	queries  sync.Map // queryId -> context.CancelFunc
}

type Options struct {
	DataDir       string
	MigrationsDir string
	In            io.Reader
	Out           io.Writer
	Log           *slog.Logger
}

func New(opts Options) (*App, error) {
	log := opts.Log
	if log == nil {
		log = logging.New(os.Stderr, slog.LevelInfo)
	}
	dataDir := opts.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".relay")
	}
	// An empty MigrationsDir uses the migrations embedded in the binary.
	migrDir := opts.MigrationsDir

	db, err := storage.Open(dataDir, migrDir)
	if err != nil {
		return nil, err
	}
	secrets, backend, err := security.NewDefaultKeyStore(dataDir)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	log.Info("keystore ready", "backend", backend)
	if backend == "file" {
		log.Warn("OS keychain unavailable; secrets are stored in a local file with owner-only permissions")
	}
	if db.Recovered {
		log.Warn("database was unreadable and has been moved aside", "path", db.RecoveredPath)
	}

	repo := storage.NewConnectionRepo(db)
	if err := repo.ResetRuntimeStatuses(); err != nil {
		log.Warn("failed to reset stale connection statuses", "error", err)
	}
	conns := connections.NewManager(repo, secrets)
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	server := ipc.NewServer(in, out, log)

	a := &App{
		log:      log,
		server:   server,
		db:       db,
		conns:    conns,
		secrets:  secrets,
		prefs:    storage.NewPreferencesRepo(db),
		dataDir:  dataDir,
		migrDir:  migrDir,
		keystore: backend,
	}
	a.msgs = messages.NewManager(conns, func(event, corr string, payload any) {
		ev := ipc.NewEvent(event, payload)
		ev.CorrelationID = corr
		_ = server.Emit(ev)
	})
	conns.SetDisconnectHook(a.msgs.StopForConnection)
	conns.SetStatusListener(func(connectionID string, status broker.ConnectionStatus, detail string) {
		_ = server.Emit(ipc.NewEvent("connection.status", map[string]any{
			"connectionId": connectionID,
			"status":       status,
			"detail":       detail,
		}))
	})
	insightsStore := monitoring.NewStore(db)
	a.insights = monitoring.NewManager(insightsStore, func(event string, payload any) {
		_ = server.Emit(ipc.NewEvent(event, payload))
	})
	a.insights.StartEnabled()
	a.registerHandlers()
	return a, nil
}

func (a *App) Run(ctx context.Context) error {
	ready := ipc.Event{
		Type:            ipc.TypeEvent,
		Event:           "engine.ready",
		TS:              time.Now().UTC(),
		ProtocolVersion: ipc.ProtocolVersion,
		EngineVersion:   EngineVersion,
	}
	if err := a.server.Emit(ready); err != nil {
		return err
	}
	return a.server.Run(ctx)
}

func (a *App) Shutdown(ctx context.Context) {
	a.shutdown.Do(func() {
		if a.insights != nil {
			a.insights.Shutdown()
		}
		a.msgs.Shutdown()
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		a.conns.Shutdown(sctx)
		a.server.Close()
		_ = a.db.Close()
	})
}

func (a *App) registerHandlers() {
	s := a.server

	s.Handle("hello", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ProtocolVersion int    `json:"protocolVersion"`
			Client          string `json:"client"`
			ClientVersion   string `json:"clientVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		if params.ProtocolVersion != 0 && params.ProtocolVersion != ipc.ProtocolVersion {
			return nil, broker.NewError(broker.ErrValidation, "incompatible protocol version", false)
		}
		return map[string]any{
			"protocolVersion":  ipc.ProtocolVersion,
			"engineVersion":    EngineVersion,
			"capabilities":     []string{"nats", "insights"},
			"schemaVersion":    a.db.SchemaVersion(),
			"keystore":         a.keystore,
			"storageRecovered": a.db.Recovered,
			"dataDir":          a.dataDir,
		}, nil
	})

	s.Handle("ping", func(ctx context.Context, req ipc.Request) (any, error) {
		return map[string]any{"pong": true, "ts": time.Now().UTC()}, nil
	})

	s.Handle("cancel", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			TargetID string `json:"targetId"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.TargetID == "" {
			return nil, broker.NewError(broker.ErrValidation, "targetId required", false)
		}
		ok := s.Cancel(params.TargetID)
		if !ok {
			_ = a.msgs.Unsubscribe(params.TargetID)
		}
		return map[string]any{"cancelled": true}, nil
	})

	s.Handle("shutdown", func(ctx context.Context, req ipc.Request) (any, error) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			a.Shutdown(context.Background())
			os.Exit(0)
		}()
		return map[string]any{"ok": true}, nil
	})

	s.Handle("connections.list", func(ctx context.Context, req ipc.Request) (any, error) {
		list, err := a.conns.List()
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []broker.ConnectionProfile{}
		}
		return map[string]any{"connections": list}, nil
	})

	s.Handle("connections.get", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Get(params.ID)
	})

	s.Handle("connections.create", func(ctx context.Context, req ipc.Request) (any, error) {
		var in connections.UpsertInput
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Create(in)
	})

	s.Handle("connections.update", func(ctx context.Context, req ipc.Request) (any, error) {
		var in connections.UpsertInput
		if err := json.Unmarshal(req.Params, &in); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Update(in)
	})

	s.Handle("connections.delete", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		if err := a.conns.Delete(params.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil
	})

	s.Handle("connections.duplicate", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Duplicate(params.ID)
	})

	s.Handle("connections.connect", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID     string `json:"id"`
			Secret string `json:"secret,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Connect(ctx, params.ID, params.Secret)
	})

	s.Handle("connections.disconnect", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		if err := a.conns.Disconnect(ctx, params.ID); err != nil {
			return nil, err
		}
		return map[string]any{"disconnected": true}, nil
	})

	s.Handle("connections.test", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID      string                   `json:"id,omitempty"`
			Profile broker.ConnectionProfile `json:"profile"`
			Secret  string                   `json:"secret,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Test(ctx, params.ID, params.Profile, params.Secret)
	})

	s.Handle("connections.status", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		st, err := a.conns.Status(params.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"connectionId": params.ID, "status": st}, nil
	})

	s.Handle("connections.health", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		sess, err := a.conns.Session(params.ID)
		if err != nil {
			return nil, err
		}
		return sess.Adapter.Health(ctx)
	})

	s.Handle("connections.capabilities", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.conns.Capabilities(params.ID)
	})

	s.Handle("messages.subscribe", func(ctx context.Context, req ipc.Request) (any, error) {
		var params broker.SubscribeRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		// Subscriptions are long-lived and cancelled via messages.unsubscribe / cancel(targetId=subscriptionId).
		return a.msgs.Subscribe(context.Background(), params)
	})

	s.Handle("messages.unsubscribe", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		if err := a.msgs.Unsubscribe(params.ID); err != nil {
			return nil, err
		}
		s.Cancel(params.ID)
		return map[string]any{"unsubscribed": true}, nil
	})

	s.Handle("messages.subscriptions", func(ctx context.Context, req ipc.Request) (any, error) {
		return map[string]any{"subscriptions": a.msgs.List()}, nil
	})

	s.Handle("messages.pause", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		return map[string]any{"ok": true}, a.msgs.Pause(params.ID)
	})

	s.Handle("messages.resume", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		return map[string]any{"ok": true}, a.msgs.Resume(params.ID)
	})

	s.Handle("messages.publish", func(ctx context.Context, req ipc.Request) (any, error) {
		var msg broker.OutboundMessage
		if err := json.Unmarshal(req.Params, &msg); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		return a.msgs.Publish(ctx, msg)
	})

	s.Handle("messages.request", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			broker.OutboundMessage
			TimeoutMs int `json:"timeoutMs"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		if params.TimeoutMs <= 0 {
			params.TimeoutMs = 5000
		}
		return a.msgs.Request(ctx, params.OutboundMessage, params.TimeoutMs)
	})

	s.Handle("explorer.list", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ConnectionID string `json:"connectionId"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, broker.NewError(broker.ErrValidation, "invalid params", false)
		}
		sess, err := a.conns.Session(params.ConnectionID)
		if err != nil {
			return nil, err
		}
		ex, ok := sess.Adapter.(broker.ResourceExplorer)
		if !ok {
			return map[string]any{"resources": []any{}}, nil
		}
		resources, err := ex.ListResources(ctx)
		if err != nil {
			return nil, err
		}
		if resources == nil {
			resources = []broker.ResourceDescriptor{}
		}
		return map[string]any{"resources": resources}, nil
	})

	s.Handle("jetstream.streams.list", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ConnectionID string `json:"connectionId"`
		}
		_ = json.Unmarshal(req.Params, &params)
		sess, err := a.conns.Session(params.ConnectionID)
		if err != nil {
			return nil, err
		}
		js, ok := sess.Adapter.(broker.JetStreamExplorer)
		if !ok {
			return nil, broker.NewError(broker.ErrUnsupported, "JetStream not supported", false)
		}
		streams, err := js.ListStreams(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"streams": streams}, nil
	})

	s.Handle("jetstream.streams.get", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ConnectionID string `json:"connectionId"`
			Name         string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &params)
		sess, err := a.conns.Session(params.ConnectionID)
		if err != nil {
			return nil, err
		}
		js, ok := sess.Adapter.(broker.JetStreamExplorer)
		if !ok {
			return nil, broker.NewError(broker.ErrUnsupported, "JetStream not supported", false)
		}
		return js.GetStream(ctx, params.Name)
	})

	s.Handle("jetstream.consumers.list", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ConnectionID string `json:"connectionId"`
			Stream       string `json:"stream"`
		}
		_ = json.Unmarshal(req.Params, &params)
		sess, err := a.conns.Session(params.ConnectionID)
		if err != nil {
			return nil, err
		}
		js, ok := sess.Adapter.(broker.JetStreamExplorer)
		if !ok {
			return nil, broker.NewError(broker.ErrUnsupported, "JetStream not supported", false)
		}
		list, err := js.ListConsumers(ctx, params.Stream)
		if err != nil {
			return nil, err
		}
		return map[string]any{"consumers": list}, nil
	})

	s.Handle("jetstream.messages.get", func(ctx context.Context, req ipc.Request) (any, error) {
		var params struct {
			ConnectionID string `json:"connectionId"`
			Stream       string `json:"stream"`
			Sequence     uint64 `json:"sequence"`
		}
		_ = json.Unmarshal(req.Params, &params)
		sess, err := a.conns.Session(params.ConnectionID)
		if err != nil {
			return nil, err
		}
		js, ok := sess.Adapter.(broker.JetStreamExplorer)
		if !ok {
			return nil, broker.NewError(broker.ErrUnsupported, "JetStream not supported", false)
		}
		return js.GetMessage(ctx, params.Stream, params.Sequence)
	})

	s.Handle("preferences.get", func(ctx context.Context, req ipc.Request) (any, error) {
		return a.prefs.Get()
	})

	s.Handle("preferences.set", func(ctx context.Context, req ipc.Request) (any, error) {
		return a.prefs.Patch(req.Params)
	})

	a.registerInsightsHandlers()
}
