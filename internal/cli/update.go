package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kshehadeh/store/internal/update"
)

func newUpdate() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update store to the latest release",
		Long: `Update store to the latest GitHub release.

The download is verified against the release's published SHA-256 checksums
before it replaces the running binary. When store was installed with Homebrew,
this runs brew upgrade --cask ` + update.BrewCask + ` instead.

Use --check to report whether a newer release exists without installing it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release is available")
	return cmd
}

func runUpdate(cmd *cobra.Command, check bool) error {
	current := commandVersion()
	if !update.IsRelease(current) {
		return fmt.Errorf("this build of store (%s) is not a release, so it cannot update itself; install a release instead", current)
	}
	client := update.NewClient()
	rel, err := client.Latest(cmd.Context())
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if !update.Newer(rel.Version, current) {
		fmt.Fprintf(out, "store %s is up to date.\n", current)
		return nil
	}
	if check {
		fmt.Fprintf(out, "store %s is available (you have %s). Run `store update` to install it.\n", rel.Version, current)
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if update.IsHomebrew(exe) {
		return brewUpgrade(cmd)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Downloading store %s...\n", rel.Version)
	if err := client.Install(cmd.Context(), rel, exe, runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated store from %s to %s.\n", current, rel.Version)
	return nil
}

func brewUpgrade(cmd *cobra.Command) error {
	args := []string{"upgrade", "--cask", update.BrewCask}
	brew, err := exec.LookPath("brew")
	if err != nil {
		return fmt.Errorf("store was installed with Homebrew, but brew is not on PATH; run: brew %s", strings.Join(args, " "))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "store was installed with Homebrew; running: brew %s\n", strings.Join(args, " "))
	c := exec.CommandContext(cmd.Context(), brew, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("brew upgrade failed with exit code %d", exit.ExitCode())
		}
		return err
	}
	return nil
}
