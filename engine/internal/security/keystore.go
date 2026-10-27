package security

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"
)

const serviceName = "relay-ide"

// KeyStore abstracts OS credential storage with a file-backed fallback for CI/dev.
type KeyStore interface {
	Set(ref, secret string) error
	Get(ref string) (string, error)
	Delete(ref string) error
}

type OSKeyStore struct {
	service string
}

func NewOSKeyStore() *OSKeyStore {
	return &OSKeyStore{service: serviceName}
}

func (k *OSKeyStore) Set(ref, secret string) error {
	return keyring.Set(k.service, ref, secret)
}

func (k *OSKeyStore) Get(ref string) (string, error) {
	return keyring.Get(k.service, ref)
}

func (k *OSKeyStore) Delete(ref string) error {
	err := keyring.Delete(k.service, ref)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// FileKeyStore is a restricted fallback for headless CI. Files are mode 0600.
type FileKeyStore struct {
	dir string
	mu  sync.Mutex
}

func NewFileKeyStore(dir string) (*FileKeyStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &FileKeyStore{dir: dir}, nil
}

func (f *FileKeyStore) path(ref string) string {
	return filepath.Join(f.dir, filepath.Base(ref)+".secret")
}

func (f *FileKeyStore) Set(ref, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return os.WriteFile(f.path(ref), []byte(secret), 0o600)
}

func (f *FileKeyStore) Get(ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := os.ReadFile(f.path(ref))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (f *FileKeyStore) Delete(ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := os.Remove(f.path(ref))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// NewDefaultKeyStore prefers OS keyring; falls back to file store under dataDir/secrets.
func NewDefaultKeyStore(dataDir string) (KeyStore, string, error) {
	osks := NewOSKeyStore()
	// Probe keyring availability
	probeRef := "__relay_probe__"
	if err := osks.Set(probeRef, "ok"); err == nil {
		_ = osks.Delete(probeRef)
		return osks, "os", nil
	}
	fs, err := NewFileKeyStore(filepath.Join(dataDir, "secrets"))
	if err != nil {
		return nil, "", fmt.Errorf("keystore unavailable: %w", err)
	}
	return fs, "file", nil
}
