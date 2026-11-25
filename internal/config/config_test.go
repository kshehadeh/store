package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMigratesV1(t *testing.T) {
	path := writeFile(t, `{"version":1,"provider":"1password","settings":{"vault_id":"v1","account":"ACCT"}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 2 || cfg.Default != "default" || len(cfg.Connections) != 1 {
		t.Fatalf("migrated config = %+v", cfg)
	}
	conn := cfg.Connections["default"]
	if conn.Provider != "1password" || conn.Settings["vault_id"] != "v1" || conn.Settings["account"] != "ACCT" {
		t.Fatalf("migrated connection = %+v", conn)
	}
}

func TestLoadNotConfigured(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err != ErrNotConfigured {
		t.Fatalf("missing file err = %v", err)
	}
	if _, err := Load(writeFile(t, `{"version":2,"connections":{}}`)); err != ErrNotConfigured {
		t.Fatalf("empty connections err = %v", err)
	}
}

func TestSelect(t *testing.T) {
	cfg := &Config{Connections: map[string]Connection{
		"work":     {Provider: "1password"},
		"personal": {Provider: "bitwarden"},
	}}
	if _, _, err := cfg.Select(""); err == nil {
		t.Fatal("Select with no default should fail")
	}
	cfg.Default = "work"
	id, conn, err := cfg.Select("")
	if err != nil || id != "work" || conn.Provider != "1password" {
		t.Fatalf("Select default = %q %+v %v", id, conn, err)
	}
	id, conn, err = cfg.Select("personal")
	if err != nil || id != "personal" || conn.Provider != "bitwarden" {
		t.Fatalf("Select named = %q %+v %v", id, conn, err)
	}
	if _, _, err := cfg.Select("nope"); err == nil {
		t.Fatal("Select unknown should fail")
	}
}

func TestAddRemoveAndDefault(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Add("work", Connection{Provider: "1password"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "work" {
		t.Fatalf("first connection should become default, got %q", cfg.Default)
	}
	if err := cfg.Add("Bad ID", Connection{}); err == nil {
		t.Fatal("invalid id accepted")
	}
	if err := cfg.Add("work", Connection{}); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if err := cfg.Add("bw", Connection{Provider: "bitwarden"}); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "work" {
		t.Fatalf("default changed to %q", cfg.Default)
	}
	if err := cfg.SetDefault("bw"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefault("nope"); err == nil {
		t.Fatal("SetDefault unknown accepted")
	}
	if err := cfg.Remove("bw"); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "work" {
		t.Fatalf("default after remove = %q, want work", cfg.Default)
	}
	if err := cfg.Remove("work"); err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "" || len(cfg.Connections) != 0 {
		t.Fatalf("config after removing all = %+v", cfg)
	}
	if err := cfg.Remove("work"); err == nil {
		t.Fatal("Remove unknown accepted")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store", "config.json")
	cfg := &Config{Default: "work", Connections: map[string]Connection{
		"work": {Provider: "1password", Settings: map[string]string{"vault_id": "v1"}},
	}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode: %v %v", info, err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Default != "work" || got.Connections["work"].Settings["vault_id"] != "v1" {
		t.Fatalf("round trip = %+v", got)
	}
}
