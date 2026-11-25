// Package config stores the vault connections this computer uses.
// It holds identifiers only; authentication stays with the provider's tools.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Connection is one named vault location: a provider plus its settings.
type Connection struct {
	Provider string            `json:"provider"`
	Settings map[string]string `json:"settings"`
}

// Config holds every connection and which one is used by default.
type Config struct {
	Version     int                   `json:"version"`
	Default     string                `json:"default"`
	Connections map[string]Connection `json:"connections"`
}

var ErrNotConfigured = errors.New("store is not set up on this computer; run `store connection add` (or `store setup`)")

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateID reports whether id can name a connection.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid connection id %q: use lowercase letters, digits, and dashes, starting with a letter or digit", id)
	}
	return nil
}

// Path returns the configuration file location. STORE_CONFIG overrides it.
func Path() (string, error) {
	if p := os.Getenv("STORE_CONFIG"); p != "" {
		return p, nil
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "store", "config.json"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "store", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "store", "config.json"), nil
}

// fileV1 is the original single-vault configuration shape.
type fileV1 struct {
	Provider string            `json:"provider"`
	Settings map[string]string `json:"settings"`
}

// Load reads the configuration, upgrading the original single-connection
// format in memory. The file itself is rewritten on the next Save.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w; run `store connection add` to recreate it", path, err)
	}
	if len(c.Connections) == 0 {
		var old fileV1
		if err := json.Unmarshal(data, &old); err == nil && old.Provider != "" {
			c.Connections = map[string]Connection{"default": {Provider: old.Provider, Settings: old.Settings}}
			c.Default = "default"
		}
	}
	if len(c.Connections) == 0 {
		return nil, ErrNotConfigured
	}
	c.Version = 2
	return &c, nil
}

func Save(path string, c *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	c.Version = 2
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// IDs returns the connection ids in sorted order.
func (c *Config) IDs() []string {
	ids := make([]string, 0, len(c.Connections))
	for id := range c.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Select resolves a connection by id, or the default when id is empty.
func (c *Config) Select(id string) (string, Connection, error) {
	if len(c.Connections) == 0 {
		return "", Connection{}, ErrNotConfigured
	}
	if id == "" {
		id = c.Default
		if id == "" {
			return "", Connection{}, fmt.Errorf("no default connection is set; pick one with --connection or set it with `store connection default` (available: %s)", strings.Join(c.IDs(), ", "))
		}
	}
	conn, ok := c.Connections[id]
	if !ok {
		return "", Connection{}, fmt.Errorf("unknown connection %q; available: %s (see `store connection list`)", id, strings.Join(c.IDs(), ", "))
	}
	return id, conn, nil
}

// Add stores a connection. The first connection becomes the default.
func (c *Config) Add(id string, conn Connection) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if c.Connections == nil {
		c.Connections = map[string]Connection{}
	}
	if _, exists := c.Connections[id]; exists {
		return fmt.Errorf("connection %q already exists; remove it first with `store connection remove %s`", id, id)
	}
	c.Connections[id] = conn
	if c.Default == "" {
		c.Default = id
	}
	return nil
}

// Remove deletes a connection. If it was the default, the alphabetically
// first remaining connection becomes the default.
func (c *Config) Remove(id string) error {
	if _, ok := c.Connections[id]; !ok {
		return fmt.Errorf("unknown connection %q; available: %s", id, strings.Join(c.IDs(), ", "))
	}
	delete(c.Connections, id)
	if c.Default == id {
		c.Default = ""
		if ids := c.IDs(); len(ids) > 0 {
			c.Default = ids[0]
		}
	}
	return nil
}

// SetDefault makes an existing connection the default.
func (c *Config) SetDefault(id string) error {
	if _, ok := c.Connections[id]; !ok {
		return fmt.Errorf("unknown connection %q; available: %s", id, strings.Join(c.IDs(), ", "))
	}
	c.Default = id
	return nil
}
