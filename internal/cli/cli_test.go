package cli

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	root := newRoot()
	root.Version = "v0.1.0"
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "store version v0.1.0" {
		t.Fatalf("version output = %q", got)
	}
}

func TestTUICommandIsRegistered(t *testing.T) {
	cmd, _, err := newRoot().Find([]string{"tui"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Use != "tui" || !strings.Contains(cmd.Long, "Ctrl-R to rename") {
		t.Fatalf("tui command = %q, %q", cmd.Use, cmd.Long)
	}
}

func TestResolveVersion(t *testing.T) {
	for _, tc := range []struct {
		override string
		module   string
		want     string
	}{
		{"dev", "v0.1.0", "v0.1.0"},
		{"v0.2.0", "v0.1.0", "v0.2.0"},
		{"dev", "(devel)", "dev"},
		{"dev", "", "dev"},
	} {
		info := &debug.BuildInfo{Main: debug.Module{Version: tc.module}}
		if got := resolveVersion(tc.override, info); got != tc.want {
			t.Errorf("resolveVersion(%q, %q) = %q; want %q", tc.override, tc.module, got, tc.want)
		}
	}
	if got := resolveVersion("dev", nil); got != "dev" {
		t.Errorf("version without build info = %q", got)
	}
}
