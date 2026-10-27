package connections

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nkeys"
	natsadapter "github.com/relay-ide/relay/engine/internal/adapters/nats"
	"github.com/relay-ide/relay/engine/internal/broker"
	"github.com/relay-ide/relay/engine/internal/security"
	"github.com/relay-ide/relay/engine/internal/storage"
)

type StatusListener func(connectionID string, status broker.ConnectionStatus, detail string)

type Session struct {
	Profile broker.ConnectionProfile
	Adapter broker.BrokerAdapter
	Status  broker.ConnectionStatus
}

type Manager struct {
	repo         *storage.ConnectionRepo
	secrets      security.KeyStore
	mu           sync.RWMutex
	sessions     map[string]*Session
	connecting   map[string]bool
	onStatus     StatusListener
	onDisconnect func(connectionID string)
	newAdapterFn func(broker.BrokerType) (broker.BrokerAdapter, error)
}

func NewManager(repo *storage.ConnectionRepo, secrets security.KeyStore) *Manager {
	m := &Manager{
		repo:       repo,
		secrets:    secrets,
		sessions:   make(map[string]*Session),
		connecting: make(map[string]bool),
	}
	m.newAdapterFn = m.defaultAdapter
	return m
}

func (m *Manager) SetStatusListener(fn StatusListener) { m.onStatus = fn }

// SetDisconnectHook registers cleanup that must run when a session ends
// (e.g. stopping its subscriptions).
func (m *Manager) SetDisconnectHook(fn func(connectionID string)) { m.onDisconnect = fn }

func (m *Manager) emit(id string, status broker.ConnectionStatus, detail string) {
	if m.onStatus != nil {
		m.onStatus(id, status, security.RedactURL(detail))
	}
}

func secretRef(id string) string { return "conn:" + id + ":secret" }

func needsSecret(method broker.AuthMethod) bool {
	switch method {
	case broker.AuthUserPass, broker.AuthToken, broker.AuthNKey:
		return true
	}
	return false
}

// redact strips anything credential-like from a profile before it leaves the
// engine. Profiles created before URL validation existed may still contain
// user:pass@ in server URLs.
func redact(p broker.ConnectionProfile) broker.ConnectionProfile {
	servers := make([]string, len(p.Servers))
	for i, s := range p.Servers {
		servers[i] = security.RedactURL(s)
	}
	p.Servers = servers
	p.Auth.EphemeralSecret = ""
	return p
}

func (m *Manager) List() ([]broker.ConnectionProfile, error) {
	list, err := m.repo.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i] = redact(list[i])
		list[i].LastStatus = m.liveStatus(list[i].ID, list[i].LastStatus)
	}
	return list, nil
}

func (m *Manager) Get(id string) (*broker.ConnectionProfile, error) {
	p, err := m.repo.Get(id)
	if err != nil {
		return nil, err
	}
	r := redact(*p)
	r.LastStatus = m.liveStatus(id, r.LastStatus)
	return &r, nil
}

func (m *Manager) liveStatus(id string, fallback broker.ConnectionStatus) broker.ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[id]; ok {
		return s.Status
	}
	if m.connecting[id] {
		return broker.StatusConnecting
	}
	return fallback
}

type UpsertInput struct {
	Profile broker.ConnectionProfile `json:"profile"`
	Secret  string                   `json:"secret,omitempty"`
	// ClearSecret removes the stored secret on update.
	ClearSecret bool `json:"clearSecret,omitempty"`
}

var supportedSchemes = []string{"nats", "tls", "ws", "wss"}

// ValidateProfile normalizes and validates a profile. It never inspects
// stored secrets.
func ValidateProfile(p *broker.ConnectionProfile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return broker.NewError(broker.ErrValidation, "name is required", false)
	}
	if len(p.Name) > 120 {
		return broker.NewError(broker.ErrValidation, "name must be at most 120 characters", false)
	}
	if p.BrokerType == "" {
		p.BrokerType = broker.BrokerNATS
	}
	if p.BrokerType != broker.BrokerNATS {
		return broker.NewError(broker.ErrUnsupported, "only NATS connections are supported in this version", false)
	}
	if p.Environment == "" {
		p.Environment = broker.EnvLocal
	}
	switch p.Environment {
	case broker.EnvLocal, broker.EnvDevelopment, broker.EnvStaging, broker.EnvProduction, broker.EnvCustom:
	default:
		return broker.NewError(broker.ErrValidation, "unknown environment", false)
	}
	servers := make([]string, 0, len(p.Servers))
	seen := map[string]bool{}
	for _, raw := range p.Servers {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "://") {
			s = "nats://" + s
		}
		if err := security.ValidateEndpointURL(s, supportedSchemes...); err != nil {
			return broker.NewError(broker.ErrValidation, "server "+security.RedactURL(s)+": "+err.Error(), false)
		}
		if !seen[s] {
			seen[s] = true
			servers = append(servers, s)
		}
	}
	if len(servers) == 0 {
		return broker.NewError(broker.ErrValidation, "at least one server URL is required", false)
	}
	if len(servers) > 32 {
		return broker.NewError(broker.ErrValidation, "at most 32 server URLs are allowed", false)
	}
	p.Servers = servers

	if p.TimeoutMs == 0 {
		p.TimeoutMs = 10000
	}
	if p.TimeoutMs < 500 || p.TimeoutMs > 120000 {
		return broker.NewError(broker.ErrValidation, "connect timeout must be between 500 ms and 120 s", false)
	}
	r := &p.Reconnect
	if r.InitialBackoffMs == 0 {
		r.InitialBackoffMs = 500
	}
	if r.MaxBackoffMs == 0 {
		r.MaxBackoffMs = 10000
	}
	if r.InitialBackoffMs < 50 || r.InitialBackoffMs > 60000 || r.MaxBackoffMs < r.InitialBackoffMs || r.MaxBackoffMs > 300000 {
		return broker.NewError(broker.ErrValidation, "reconnect backoff must satisfy 50 ms ≤ initial ≤ max ≤ 5 min", false)
	}
	if r.MaxAttempts < 0 || r.MaxAttempts > 100000 {
		return broker.NewError(broker.ErrValidation, "max reconnect attempts must be 0 (unlimited) to 100000", false)
	}

	if p.TLS.InsecureSkipVerify {
		return broker.NewError(broker.ErrTLS, "disabling TLS certificate verification is not allowed", false)
	}
	for label, path := range map[string]string{"CA certificate": p.TLS.CACertPath, "client certificate": p.TLS.ClientCertPath, "client key": p.TLS.ClientKeyPath, "credentials file": p.Auth.CredsPath} {
		if err := checkFile(label, path); err != nil {
			return err
		}
	}
	if (p.TLS.ClientCertPath == "") != (p.TLS.ClientKeyPath == "") {
		return broker.NewError(broker.ErrValidation, "client certificate and key must be provided together", false)
	}

	switch p.Auth.Method {
	case "", broker.AuthNone:
		p.Auth.Method = broker.AuthNone
	case broker.AuthUserPass:
		p.Auth.Username = strings.TrimSpace(p.Auth.Username)
		if p.Auth.Username == "" {
			return broker.NewError(broker.ErrValidation, "username is required", false)
		}
	case broker.AuthToken, broker.AuthNKey:
	case broker.AuthCreds:
		if p.Auth.CredsPath == "" {
			return broker.NewError(broker.ErrValidation, "credentials file is required", false)
		}
	case broker.AuthTLSCert:
		if !p.TLS.Enabled || p.TLS.ClientCertPath == "" {
			return broker.NewError(broker.ErrValidation, "certificate authentication requires TLS with a client certificate and key", false)
		}
	default:
		return broker.NewError(broker.ErrValidation, "unsupported authentication method", false)
	}
	if !needsSecret(p.Auth.Method) {
		p.Auth.Username = strings.TrimSpace(p.Auth.Username)
	}
	return nil
}

func checkFile(label, path string) error {
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return broker.NewError(broker.ErrValidation, label+" not found: "+path, false)
	}
	if !st.Mode().IsRegular() {
		return broker.NewError(broker.ErrValidation, label+" must be a regular file", false)
	}
	if st.Size() > 1<<20 {
		return broker.NewError(broker.ErrValidation, label+" is unexpectedly large (over 1 MiB)", false)
	}
	return nil
}

func validateSecret(method broker.AuthMethod, secret string) error {
	if secret == "" || method != broker.AuthNKey {
		return nil
	}
	kp, err := nkeys.FromSeed([]byte(strings.TrimSpace(secret)))
	if err != nil {
		return broker.NewError(broker.ErrValidation, "invalid NKey seed", false)
	}
	pub, err := kp.PublicKey()
	if err != nil || !nkeys.IsValidPublicUserKey(pub) {
		return broker.NewError(broker.ErrValidation, "NKey seed is not a user seed (expected SU...)", false)
	}
	return nil
}

// applySecretRefs sets the auth refs from server-side state only; refs sent by
// a client are never trusted.
func applySecretRefs(p *broker.ConnectionProfile, hasSecret bool) {
	p.Auth.SecretRef, p.Auth.TokenRef, p.Auth.NKeySeedRef, p.Auth.EphemeralSecret = "", "", "", ""
	if !hasSecret {
		return
	}
	ref := secretRef(p.ID)
	p.Auth.SecretRef = ref
	switch p.Auth.Method {
	case broker.AuthToken:
		p.Auth.TokenRef = ref
	case broker.AuthNKey:
		p.Auth.NKeySeedRef = ref
	}
}

func (m *Manager) Create(in UpsertInput) (*broker.ConnectionProfile, error) {
	p := in.Profile
	if err := ValidateProfile(&p); err != nil {
		return nil, err
	}
	if err := validateSecret(p.Auth.Method, in.Secret); err != nil {
		return nil, err
	}
	p.ID = uuid.NewString()
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	p.LastStatus = broker.StatusDisconnected
	p.LastConnectedAt = nil

	hasSecret := false
	if in.Secret != "" && needsSecret(p.Auth.Method) {
		if err := m.secrets.Set(secretRef(p.ID), in.Secret); err != nil {
			return nil, broker.Wrap(broker.ErrInternal, "failed to store secret", false, err)
		}
		hasSecret = true
	}
	applySecretRefs(&p, hasSecret)
	if err := m.repo.Upsert(p); err != nil {
		if hasSecret {
			_ = m.secrets.Delete(secretRef(p.ID))
		}
		return nil, err
	}
	r := redact(p)
	return &r, nil
}

func (m *Manager) Update(in UpsertInput) (*broker.ConnectionProfile, error) {
	existing, err := m.repo.Get(in.Profile.ID)
	if err != nil {
		return nil, err
	}
	p := in.Profile
	if err := ValidateProfile(&p); err != nil {
		return nil, err
	}
	if err := validateSecret(p.Auth.Method, in.Secret); err != nil {
		return nil, err
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	p.LastConnectedAt = existing.LastConnectedAt
	p.LastStatus = existing.LastStatus

	hasSecret := existing.Auth.SecretRef != ""
	switch {
	case !needsSecret(p.Auth.Method) || in.ClearSecret:
		if hasSecret {
			_ = m.secrets.Delete(existing.Auth.SecretRef)
		}
		hasSecret = false
	case in.Secret != "":
		if err := m.secrets.Set(secretRef(p.ID), in.Secret); err != nil {
			return nil, broker.Wrap(broker.ErrInternal, "failed to store secret", false, err)
		}
		hasSecret = true
	case existing.Auth.Method != p.Auth.Method && hasSecret:
		// A password is not a valid token or seed; require a new secret.
		_ = m.secrets.Delete(existing.Auth.SecretRef)
		hasSecret = false
	}
	applySecretRefs(&p, hasSecret)
	if err := m.repo.Upsert(p); err != nil {
		return nil, err
	}
	r := redact(p)
	r.LastStatus = m.liveStatus(p.ID, r.LastStatus)
	return &r, nil
}

func (m *Manager) Delete(id string) error {
	p, err := m.repo.Get(id)
	if err != nil {
		return err
	}
	_ = m.Disconnect(context.Background(), id)
	if p.Auth.SecretRef != "" {
		_ = m.secrets.Delete(p.Auth.SecretRef)
	}
	return m.repo.Delete(id)
}

func (m *Manager) Duplicate(id string) (*broker.ConnectionProfile, error) {
	p, err := m.repo.Get(id)
	if err != nil {
		return nil, err
	}
	secret := ""
	if p.Auth.SecretRef != "" {
		secret, _ = m.secrets.Get(p.Auth.SecretRef)
	}
	cp := *p
	cp.Name = p.Name + " (copy)"
	if len(cp.Name) > 120 {
		cp.Name = cp.Name[:120]
	}
	return m.Create(UpsertInput{Profile: cp, Secret: secret})
}

func (m *Manager) defaultAdapter(t broker.BrokerType) (broker.BrokerAdapter, error) {
	switch t {
	case broker.BrokerNATS:
		return natsadapter.New(), nil
	default:
		return nil, broker.NewError(broker.ErrUnsupported, "broker type not implemented: "+string(t), false)
	}
}

// resolveSecrets returns the secret for a stored profile (by id), unless the
// caller supplied one for this attempt.
func (m *Manager) resolveSecrets(p broker.ConnectionProfile, storedRef, ephemeral string) (map[string]string, error) {
	out := map[string]string{}
	if !needsSecret(p.Auth.Method) {
		return out, nil
	}
	secret := ephemeral
	if secret == "" && storedRef != "" {
		s, err := m.secrets.Get(storedRef)
		if err != nil {
			return nil, broker.NewError(broker.ErrAuth, "stored secret is unavailable; re-enter it in the connection settings", false)
		}
		secret = s
	}
	if secret == "" {
		switch p.Auth.Method {
		case broker.AuthUserPass:
			return nil, broker.NewError(broker.ErrAuth, "password is required", false)
		case broker.AuthToken:
			return nil, broker.NewError(broker.ErrAuth, "token is required", false)
		case broker.AuthNKey:
			return nil, broker.NewError(broker.ErrAuth, "NKey seed is required", false)
		}
	}
	switch p.Auth.Method {
	case broker.AuthUserPass:
		out["password"] = secret
	case broker.AuthToken:
		out["token"] = secret
	case broker.AuthNKey:
		out["nkey"] = secret
	}
	return out, nil
}

func (m *Manager) setSessionStatus(id string, adapter broker.BrokerAdapter, status broker.ConnectionStatus, detail string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok || sess.Adapter != adapter {
		m.mu.Unlock()
		return // stale callback from a replaced or closed session
	}
	sess.Status = status
	m.mu.Unlock()
	var last *time.Time
	if status == broker.StatusConnected {
		now := time.Now().UTC()
		last = &now
	}
	_ = m.repo.UpdateStatus(id, status, last)
	m.emit(id, status, detail)
	if status == broker.StatusError || status == broker.StatusDisconnected {
		// The client gave up (reconnects exhausted or disabled): end the session.
		m.mu.Lock()
		if s, ok := m.sessions[id]; ok && s.Adapter == adapter {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
		if m.onDisconnect != nil {
			m.onDisconnect(id)
		}
	}
}

func (m *Manager) Connect(ctx context.Context, id string, ephemeralSecret string) (*broker.ConnectionProfile, error) {
	p, err := m.repo.Get(id)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if s, ok := m.sessions[id]; ok && (s.Status == broker.StatusConnected || s.Status == broker.StatusReconnecting) {
		m.mu.Unlock()
		r := redact(*p)
		r.LastStatus = s.Status
		return &r, nil
	}
	if m.connecting[id] {
		m.mu.Unlock()
		return nil, broker.NewError(broker.ErrValidation, "a connection attempt is already in progress", true)
	}
	m.connecting[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.connecting, id)
		m.mu.Unlock()
	}()

	adapter, err := m.newAdapterFn(p.BrokerType)
	if err != nil {
		return nil, err
	}
	secrets, err := m.resolveSecrets(*p, p.Auth.SecretRef, ephemeralSecret)
	if err != nil {
		return nil, err
	}

	m.emit(id, broker.StatusConnecting, "")
	_ = m.repo.UpdateStatus(id, broker.StatusConnecting, nil)

	cctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond+2*time.Second)
	defer cancel()
	cfg := broker.Config{
		Profile:         *p,
		ResolvedSecrets: secrets,
		OnStatus: func(st broker.ConnectionStatus, detail string) {
			m.setSessionStatus(id, adapter, st, detail)
		},
	}
	if err := adapter.Connect(cctx, cfg); err != nil {
		m.emit(id, broker.StatusError, err.Error())
		_ = m.repo.UpdateStatus(id, broker.StatusError, nil)
		return nil, err
	}

	m.mu.Lock()
	old := m.sessions[id]
	m.sessions[id] = &Session{Profile: *p, Adapter: adapter, Status: broker.StatusConnected}
	m.mu.Unlock()
	if old != nil {
		_ = old.Adapter.Disconnect(context.Background())
	}

	now := time.Now().UTC()
	p.LastConnectedAt = &now
	p.LastStatus = broker.StatusConnected
	_ = m.repo.UpdateStatus(id, broker.StatusConnected, &now)
	m.emit(id, broker.StatusConnected, "")
	r := redact(*p)
	return &r, nil
}

func (m *Manager) Disconnect(ctx context.Context, id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		m.emit(id, broker.StatusDisconnecting, "")
		if m.onDisconnect != nil {
			m.onDisconnect(id)
		}
		_ = sess.Adapter.Disconnect(ctx)
	}
	_ = m.repo.UpdateStatus(id, broker.StatusDisconnected, nil)
	m.emit(id, broker.StatusDisconnected, "")
	return nil
}

// Test connects with a throwaway adapter. When id refers to a saved
// connection and no secret is supplied, the saved secret is used; secret refs
// in the submitted profile are ignored.
func (m *Manager) Test(ctx context.Context, id string, p broker.ConnectionProfile, ephemeralSecret string) (broker.HealthStatus, error) {
	if err := ValidateProfile(&p); err != nil {
		return broker.HealthStatus{}, err
	}
	if err := validateSecret(p.Auth.Method, ephemeralSecret); err != nil {
		return broker.HealthStatus{}, err
	}
	storedRef := ""
	if id != "" {
		existing, err := m.repo.Get(id)
		if err != nil {
			return broker.HealthStatus{}, err
		}
		if existing.Auth.Method == p.Auth.Method {
			storedRef = existing.Auth.SecretRef
		}
	}
	adapter, err := m.newAdapterFn(p.BrokerType)
	if err != nil {
		return broker.HealthStatus{}, err
	}
	secrets, err := m.resolveSecrets(p, storedRef, ephemeralSecret)
	if err != nil {
		return broker.HealthStatus{}, err
	}
	p.Reconnect.Enabled = false
	cctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond+2*time.Second)
	defer cancel()
	start := time.Now()
	if err := adapter.Connect(cctx, broker.Config{Profile: p, ResolvedSecrets: secrets}); err != nil {
		return broker.HealthStatus{}, err
	}
	defer adapter.Disconnect(context.Background())
	h, err := adapter.Health(cctx)
	if err == nil {
		if h.Details == nil {
			h.Details = map[string]string{}
		}
		h.Details["connectMs"] = time.Since(start).Round(time.Millisecond).String()
	}
	return h, err
}

func (m *Manager) Session(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, broker.NewError(broker.ErrNetwork, "connection is not active", true)
	}
	cp := *s
	return &cp, nil
}

func (m *Manager) Status(id string) (broker.ConnectionStatus, error) {
	p, err := m.repo.Get(id)
	if err != nil {
		return "", err
	}
	return m.liveStatus(id, p.LastStatus), nil
}

func (m *Manager) Capabilities(id string) (broker.BrokerCapabilities, error) {
	p, err := m.repo.Get(id)
	if err != nil {
		return broker.BrokerCapabilities{}, err
	}
	adapter, err := m.newAdapterFn(p.BrokerType)
	if err != nil {
		return broker.BrokerCapabilities{}, err
	}
	caps := adapter.Capabilities()
	if p.ReadOnly {
		filtered := caps.Capabilities[:0:0]
		for _, c := range caps.Capabilities {
			if c != broker.CapPublish && c != broker.CapRequestReply {
				filtered = append(filtered, c)
			}
		}
		caps.Capabilities = filtered
		caps.Limitations = append(caps.Limitations, "This connection is read-only: publishing and request/reply are disabled.")
	}
	return caps, nil
}

func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Disconnect(ctx, id)
	}
}
