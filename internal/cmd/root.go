// Package cmd is the cobra command tree for the miabi CLI. Each file is one
// command group; all HTTP work goes through internal/api.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/miabi-io/miabi-cli/internal/ui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Persistent flags shared by every command.
var (
	flagURL       string
	flagToken     string
	flagWorkspace string
	flagJSON      bool
	flagOutput    string
	flagNoColor   bool
	flagVerbose   bool
)

var rootCmd = &cobra.Command{
	Use:   "miabi",
	Short: "Imperative client for a Miabi control panel",
	Long: "miabi drives the deploy flow against a Miabi panel's /api/v1 HTTP API.\n\n" +
		"Authenticate with MIABI_URL + MIABI_TOKEN (or `miabi login`), pick a workspace\n" +
		"with `miabi workspace switch`, and bind a default app with `miabi use <app>` so\n" +
		"app commands need no argument.",
	Example: "  miabi use web                      # bind a default app\n" +
		"  miabi apps deploy --tag $GIT_SHA --wait\n" +
		"  miabi apps logs                    # live logs of the bound app\n" +
		"  miabi apps deployments             # deploy history (by number)",
	SilenceUsage:  true,
	SilenceErrors: true,
	// Normalize output/color once for every command.
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		if flagJSON && flagOutput == "table" {
			flagOutput = "json"
		}
		// Structured output and --no-color both force plain (uncolored) text.
		if flagNoColor || structured() {
			ui.SetColor(false)
		}
	},
}

// Execute runs the CLI, mapping any error to a non-zero exit code.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		ui.Fail("%s", err)
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
	pf.StringVarP(&flagOutput, "output", "o", "table", "output format: table | json | yaml")
	pf.BoolVar(&flagJSON, "json", false, "shorthand for --output json")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable colored output")
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

// itoa is a terse strconv.Itoa for building table cells.
func itoa(n int) string { return strconv.Itoa(n) }

// structured reports whether the user asked for machine-readable output
// (--json or -o json|yaml), in which case commands skip their human rendering.
func structured() bool { return flagJSON || flagOutput == "json" || flagOutput == "yaml" }

// emit renders v in the requested structured format. Commands call it only when
// structured() is true.
func emit(v any) error {
	if flagOutput == "yaml" {
		return yaml.NewEncoder(os.Stdout).Encode(v)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// resolveAppRef resolves the target application for an app-scoped command:
// an explicit reference (the command's positional arg) wins, else the app bound
// by `miabi use`, else a helpful error. It returns the numeric id API paths use
// and the handle to display. ref is the handle or numeric id ("" to use the bound app).
func resolveAppRef(ctx context.Context, c *api.Client, eff *config.Effective, ws, ref string) (uint, string, error) {
	if ref == "" && eff.App != nil {
		if eff.App.Name != "" {
			ref = eff.App.Name
		} else if eff.App.ID != 0 {
			ref = strconv.FormatUint(uint64(eff.App.ID), 10)
		}
	}
	if ref == "" {
		return 0, "", fmt.Errorf("no application specified — pass it as the first argument, or bind a default with `miabi use <app>`")
	}
	id, err := c.ResolveAppID(ctx, ws, ref)
	if err != nil {
		return 0, "", err
	}
	return id, ref, nil
}

// appArg pulls the optional leading app reference from a command's args: it
// returns args[0] when present (else ""), for feeding to resolveAppRef.
func appArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// completeApps is a cobra ValidArgsFunction that tab-completes app handles in the
// active workspace. Best-effort: any error yields no completions.
func completeApps(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx := context.Background()
	c, eff, err := newClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ws, err := workspaceRef(ctx, c, eff)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	apps, err := c.Apps(ctx, ws)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, a := range apps {
		if toComplete == "" || strings.HasPrefix(a.Name, toComplete) {
			out = append(out, a.Name)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
