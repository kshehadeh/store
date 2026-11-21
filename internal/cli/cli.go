// Package cli defines store's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
	"github.com/kshehadeh/store/internal/config"
	"github.com/kshehadeh/store/internal/providers"
)

// errReported signals failure after the command has already explained it.
var errReported = errors.New("reported")

// Execute runs store and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "store: %v\n", err)
		}
		return 1
	}
	return 0
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "store",
		Version:       commandVersion(),
		Short:         "Keep files, folders, and text in a secure vault and pull them on other computers",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(newSetup(), newStatus(), newKeep(), newList(), newPull())
	return root
}

func hostname() string {
	h := os.Getenv("STORE_HOSTNAME")
	if h == "" {
		h, _ = os.Hostname()
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".local")
}

func openApp(console *app.Console) (*app.App, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	def, ok := providers.Lookup(cfg.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q in %s; run `store setup`", cfg.Provider, path)
	}
	p, err := def.Open(cfg.Settings)
	if err != nil {
		return nil, err
	}
	return &app.App{
		Provider: p,
		Location: def.Describe(cfg.Settings),
		Console:  console,
		Stdout:   os.Stdout,
		Hostname: hostname(),
	}, nil
}
