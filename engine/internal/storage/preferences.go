package storage

import (
	"encoding/json"
	"fmt"

	"github.com/relay-ide/relay/engine/internal/broker"
)

// Preferences are user-facing application settings persisted in SQLite.
type Preferences struct {
	Theme                string `json:"theme"`    // dark | light | system
	Timezone             string `json:"timezone"` // local | utc
	MaxDisplayedMessages int    `json:"maxDisplayedMessages"`
	SidebarCollapsed     bool   `json:"sidebarCollapsed"`
	Density              string `json:"density"` // comfortable | compact
}

func DefaultPreferences() Preferences {
	return Preferences{
		Theme:                "dark",
		Timezone:             "local",
		MaxDisplayedMessages: 2000,
		Density:              "comfortable",
	}
}

func (p Preferences) Validate() error {
	switch p.Theme {
	case "dark", "light", "system":
	default:
		return broker.NewError(broker.ErrValidation, fmt.Sprintf("theme must be dark, light, or system (got %q)", p.Theme), false)
	}
	switch p.Timezone {
	case "local", "utc":
	default:
		return broker.NewError(broker.ErrValidation, "timezone must be local or utc", false)
	}
	if p.MaxDisplayedMessages < 100 || p.MaxDisplayedMessages > 20000 {
		return broker.NewError(broker.ErrValidation, "maxDisplayedMessages must be between 100 and 20000", false)
	}
	switch p.Density {
	case "comfortable", "compact":
	default:
		return broker.NewError(broker.ErrValidation, "density must be comfortable or compact", false)
	}
	return nil
}

type PreferencesRepo struct {
	db *DB
}

func NewPreferencesRepo(db *DB) *PreferencesRepo { return &PreferencesRepo{db: db} }

const prefsKey = "app"

// Get returns stored preferences merged over defaults. A corrupt row falls
// back to defaults rather than failing the app.
func (r *PreferencesRepo) Get() (Preferences, error) {
	p := DefaultPreferences()
	var raw string
	err := r.db.SQL.QueryRow(`SELECT value_json FROM preferences WHERE key = ?`, prefsKey).Scan(&raw)
	if err != nil {
		return p, nil
	}
	stored := p
	if json.Unmarshal([]byte(raw), &stored) != nil || stored.Validate() != nil {
		return p, nil
	}
	return stored, nil
}

// Patch applies the fields present in a JSON patch and persists the result.
func (r *PreferencesRepo) Patch(patch json.RawMessage) (Preferences, error) {
	cur, _ := r.Get()
	next := cur
	if err := json.Unmarshal(patch, &next); err != nil {
		return cur, broker.NewError(broker.ErrValidation, "invalid preferences payload", false)
	}
	if err := next.Validate(); err != nil {
		return cur, err
	}
	b, _ := json.Marshal(next)
	if _, err := r.db.SQL.Exec(`INSERT INTO preferences(key, value_json) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json`, prefsKey, string(b)); err != nil {
		return cur, err
	}
	return next, nil
}
