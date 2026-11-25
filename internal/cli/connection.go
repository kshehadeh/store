package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/app"
	"github.com/kshehadeh/store/internal/config"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/providers"
)

// connectOptions are the choices a connection add can take from flags.
type connectOptions struct {
	as, provider, account, vault string
}

func newConnection() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connection",
		Short: "Manage vault connections (add, list, set default, remove)",
		Long: `Manage the vault connections this computer can use.

Each connection points at one provider account and vault. The default
connection is used by every command unless --connection (-c) selects
another.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newConnectionAdd(), newConnectionList(), newConnectionDefault(), newConnectionRemove())
	return cmd
}

func newConnectionAdd() *cobra.Command {
	var o connectOptions
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a vault connection",
		Long: `Add a vault connection: choose the provider, account, and vault, and
verify access. Only identifiers are saved; signing in is handled by the
provider's own tools (for 1Password, the op CLI and the 1Password app).

The first connection added becomes the default. Give each further
connection a short id with --as so it can be selected with
` + "`store --connection <id>`" + ` or made the default with
` + "`store connection default <id>`" + `.`,
		Example: "  store connection add\n  store connection add --as work --vault Shared --account my.1password.com",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return addConnection(cmd, app.NewConsole(os.Stdin, os.Stderr), o)
		},
	}
	cmd.Flags().StringVar(&o.as, "as", "", "short id for this connection (default: derived from the vault name)")
	cmd.Flags().StringVar(&o.provider, "provider", "", "provider to use (available: "+providerNames(providers.All())+")")
	cmd.Flags().StringVar(&o.account, "account", "", "account to use (1Password: sign-in address, email, or account ID)")
	cmd.Flags().StringVar(&o.vault, "vault", "", "vault name or ID")
	return cmd
}

func newConnectionList() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the configured connections",
		Example: "  store connection list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			path, err := config.Path()
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			w := tabWriter(out)
			fmt.Fprintln(w, "ID\tDEFAULT\tLOCATION")
			for _, id := range cfg.IDs() {
				conn := cfg.Connections[id]
				location := conn.Provider
				if def, ok := providers.Lookup(conn.Provider); ok {
					location = def.Describe(conn.Settings)
				}
				def := ""
				if id == cfg.Default {
					def = "*"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", id, def, location)
			}
			return w.Flush()
		},
	}
}

func newConnectionDefault() *cobra.Command {
	return &cobra.Command{
		Use:     "default ID",
		Short:   "Set the default connection",
		Example: "  store connection default work",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadConfig()
			if err != nil {
				return err
			}
			if err := cfg.SetDefault(args[0]); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default connection is now %q.\n", args[0])
			return nil
		},
	}
}

func newConnectionRemove() *cobra.Command {
	return &cobra.Command{
		Use:     "remove ID",
		Short:   "Remove a connection",
		Example: "  store connection remove work",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := loadConfig()
			if err != nil {
				return err
			}
			if err := cfg.Remove(args[0]); err != nil {
				return err
			}
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Removed connection %q.\n", args[0])
			switch {
			case len(cfg.Connections) == 0:
				fmt.Fprintln(out, "No connections remain; run `store connection add` to set one up.")
			case cfg.Default != args[0]:
				fmt.Fprintf(out, "Default connection is now %q.\n", cfg.Default)
			}
			return nil
		},
	}
}

// addConnection runs the provider's setup flow and saves the result as a
// named connection. It backs both `store connection add` and `store setup`.
func addConnection(cmd *cobra.Command, console *app.Console, o connectOptions) error {
	defs := providers.All()
	var def provider.Definition
	switch {
	case o.provider != "":
		d, ok := providers.Lookup(o.provider)
		if !ok {
			return fmt.Errorf("unknown provider %q; available: %s", o.provider, providerNames(defs))
		}
		def = d
	case len(defs) == 1:
		def = defs[0]
	default:
		labels := make([]string, len(defs))
		for i, d := range defs {
			labels[i] = d.DisplayName
		}
		idx, err := console.Choose("Select a provider:", labels)
		if err != nil {
			return err
		}
		def = defs[idx]
	}
	console.Info("Provider: %s", def.DisplayName)
	preset := map[string]string{}
	if o.account != "" {
		preset["account"] = o.account
	}
	if o.vault != "" {
		preset["vault"] = o.vault
	}
	settings, err := def.Setup(cmd.Context(), console, preset)
	if err != nil {
		return err
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		if err != config.ErrNotConfigured {
			return err
		}
		cfg = &config.Config{}
	}
	id := o.as
	if id == "" {
		id = suggestID(cfg, settings)
	}
	if err := cfg.Add(id, config.Connection{Provider: def.Name, Settings: settings}); err != nil {
		return err
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Saved connection %q for %s to %s\n", id, def.Describe(settings), path)
	if cfg.Default == id && len(cfg.Connections) > 1 {
		fmt.Fprintf(out, "This is now the default connection.\n")
	} else if cfg.Default != id {
		fmt.Fprintf(out, "The default connection is %q; change it with `store connection default %s`.\n", cfg.Default, id)
	}
	fmt.Fprintln(out, "Run `store status` at any time to check connectivity.")
	return nil
}

var idCleaner = regexp.MustCompile(`[^a-z0-9]+`)

// suggestID derives a connection id from the vault name, making it unique
// against existing connections.
func suggestID(cfg *config.Config, settings map[string]string) string {
	base := idCleaner.ReplaceAllString(strings.ToLower(settings["vault_name"]), "-")
	base = strings.Trim(base, "-")
	if base == "" {
		base = "vault"
	}
	id := base
	for n := 2; ; n++ {
		if _, taken := cfg.Connections[id]; !taken {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

func tabWriter(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
}

func loadConfig() (*config.Config, string, error) {
	path, err := config.Path()
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}
	return cfg, path, nil
}
