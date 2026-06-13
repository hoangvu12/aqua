// Package config loads and persists Aqua's PC-side configuration at
// %APPDATA%\Aqua\config.json. On first run it generates a stable device id and
// a 32-byte device secret; the secret authenticates the PC to its relay room
// (trust-on-first-use) and must never leave the PC.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the on-disk PC configuration. Phase 1 uses a minimal subset; later
// phases add prepick_agent_uuid, auto_lock, ui_language, etc.
type Config struct {
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"` // never sent to the phone
	WorkerURL    string `json:"worker_url"`    // ws/wss base, e.g. wss://aqua.nguyenvu.dev

	// RemoteEnabled gates the relay connection (toggled by the console UI's `r`
	// key). When false, Aqua runs the picker locally but dials nothing out, so
	// the phone cannot reach this PC. Defaults on.
	RemoteEnabled bool `json:"remote_enabled"`

	// Picker intents (mutable from the phone via set_config).
	Enabled          bool   `json:"enabled"`
	AutoLock         bool   `json:"auto_lock"`
	PrepickAgentUUID string `json:"prepick_agent_uuid"`
	UILanguage       string `json:"ui_language"` // auto|vi|en
}

// Dir is %APPDATA%\Aqua (falls back to the OS config dir if APPDATA is unset).
func Dir() (string, error) {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "Aqua"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Aqua"), nil
}

// Path is the full path to config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config.json, creating it with freshly generated identity values on
// first run. It also backfills any missing fields and persists the result.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	var present map[string]json.RawMessage
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		_ = json.Unmarshal(data, &present) // to detect keys absent in older files
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	changed := false
	// Default remote on. A bool zero-values to false, so distinguish "absent"
	// (older config / first run) from an explicit false the user chose.
	if _, ok := present["remote_enabled"]; !ok {
		cfg.RemoteEnabled = true
		changed = true
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = newUUIDv4()
		changed = true
	}
	if cfg.DeviceSecret == "" {
		cfg.DeviceSecret = randomHex(32)
		changed = true
	}
	if cfg.WorkerURL == "" {
		cfg.WorkerURL = "wss://aqua.nguyenvu.dev"
		changed = true
	}
	if cfg.UILanguage == "" {
		cfg.UILanguage = "auto"
		cfg.Enabled = true // default the picker on for first run
		changed = true
	}
	if changed {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// Save writes the config back to disk (creating the directory if needed).
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand should never fail
	}
	return hex.EncodeToString(b)
}

func newUUIDv4() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
