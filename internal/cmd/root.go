// Package cmd is the cobra command tree for the miabi CLI. Each file is one
// command group; all HTTP work goes through internal/api.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Persistent flags shared by every command.
var (
	flagURL       string
	flagToken     string
	flagWorkspace string
	flagJSON      bool
	flagVerbose   bool
)

var rootCmd = &cobra.Command{
	Use:           "miabi",
	Short:         "Imperative client for a Miabi control panel",
	Long:          "miabi drives the deploy flow against a Miabi panel's /api/v1 HTTP API.\nAuthenticate with MIABI_URL + MIABI_TOKEN (or `miabi login`).",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI, mapping any error to a non-zero exit code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func init() {
	api.Version = version
	rootCmd.Version = version
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagURL, "url", "", "panel URL (env MIABI_URL)")
	pf.StringVar(&flagToken, "token", "", "API token (env MIABI_TOKEN)")
	pf.StringVarP(&flagWorkspace, "workspace", "w", "", "workspace name or id (default: active/bound)")
	pf.BoolVar(&flagJSON, "json", false, "machine-readable JSON output")
	pf.BoolVar(&flagVerbose, "verbose", false, "log HTTP requests to stderr")
}

// newClient resolves the connection context and returns an API client.
func newClient() (*api.Client, *config.Effective, error) {
	eff, err := config.Resolve(flagURL, flagToken)
	if err != nil {
		return nil, nil, err
	}
	return api.New(eff.URL, eff.Token, flagVerbose), eff, nil
}

// workspaceRef resolves the workspace name (handle) the API addresses by for the
// current invocation: --workspace, else the persisted active workspace, else a
// workspace-bound token's own workspace, else the sole accessible workspace.
func workspaceRef(ctx context.Context, c *api.Client, eff *config.Effective) (string, error) {
	var fallback string
	if eff.Workspace != nil {
		if eff.Workspace.Name != "" {
			fallback = eff.Workspace.Name
		} else if eff.Workspace.ID != 0 {
			// Older config without a saved name: resolve by id.
			fallback = strconv.FormatUint(uint64(eff.Workspace.ID), 10)
		}
	}
	if flagWorkspace == "" && fallback == "" {
		if me, err := c.Me(ctx); err == nil && me.Auth.WorkspaceID != nil {
			fallback = strconv.FormatUint(uint64(*me.Auth.WorkspaceID), 10)
		}
	}
	return c.ResolveWorkspaceName(ctx, flagWorkspace, fallback)
}

// printJSON writes v as indented JSON (for --json output).
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
