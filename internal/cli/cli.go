// Package cli defines store's command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/kshehadeh/store/internal/app"
	"github.com/kshehadeh/store/internal/config"
	"github.com/kshehadeh/store/internal/providers"
	"github.com/kshehadeh/store/internal/update"
)

// errReported signals failure after the command has already explained it.
var errReported = errors.New("reported")

// Execute runs store and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := newRoot()
	notice := startUpdateNotice(ctx, root, os.Args[1:])
	code := 0
	if err := root.ExecuteContext(ctx); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintf(os.Stderr, "store: %v\n", err)
		}
		code = 1
	}
	printUpdateNotice(notice)
	return code
}

// startUpdateNotice checks for a newer release in the background, only for
// release builds whose stderr is a terminal, so scripts never see a notice.
func startUpdateNotice(ctx context.Context, root *cobra.Command, args []string) <-chan string {
	if os.Getenv("STORE_NO_UPDATE_CHECK") != "" || !term.IsTerminal(int(os.Stderr.Fd())) {
		return nil
	}
	current := commandVersion()
	if !update.IsRelease(current) {
		return nil
	}
	if cmd, _, err := root.Find(args); err == nil && cmd.Name() == "update" {
		return nil
	}
	statePath, err := update.StatePath()
	if err != nil {
		return nil
	}
	n := update.Notifier{Client: update.NewClient(), StatePath: statePath, Current: current}
	ch := make(chan string, 1)
	go func() {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		ch <- n.Notice(checkCtx)
	}()
	return ch
}

// printUpdateNotice waits briefly for a pending check so quick commands are
// not held up by a slow network.
func printUpdateNotice(notice <-chan string) {
	if notice == nil {
		return
	}
	select {
	case msg := <-notice:
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	case <-time.After(time.Second):
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "store",
		Version:       commandVersion(),
		Short:         "Keep files, folders, and text in a secure vault and pull them on other computers",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Piped or redirected runs keep printing help so scripts don't
			// block on, or fail in, the interactive browser.
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return cmd.Help()
			}
			return runTUI(cmd, args)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringP("connection", "c", "", "connection to use instead of the default (see `store connection list`)")
	root.AddCommand(newSetup(), newStatus(), newKeep(), newList(), newPull(), newDelete(), newTUI(), newUpdate(), newConnection())
	return root
}

func hostname() string {
	h := os.Getenv("STORE_HOSTNAME")
	if h == "" {
		h, _ = os.Hostname()
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".local")
}

func openApp(cmd *cobra.Command, console *app.Console) (*app.App, error) {
	name, _ := cmd.Flags().GetString("connection")
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	id, conn, err := cfg.Select(name)
	if err != nil {
		return nil, err
	}
	def, ok := providers.Lookup(conn.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q in %s; run `store connection add`", conn.Provider, path)
	}
	p, err := def.Open(conn.Settings)
	if err != nil {
		return nil, err
	}
	location := def.Describe(conn.Settings)
	if len(cfg.Connections) > 1 {
		location = fmt.Sprintf("%s: %s", id, location)
	}
	return &app.App{
		Provider: p,
		Location: location,
		Console:  console,
		Stdout:   os.Stdout,
		Hostname: hostname(),
	}, nil
}
