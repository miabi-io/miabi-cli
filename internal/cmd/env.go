package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/miabi-io/miabi-cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	envSecret bool
	envFile   string
)

func init() {
	envSetCmd.Flags().BoolVar(&envSecret, "secret", false, "store the value encrypted at rest")
	envImportCmd.Flags().StringVar(&envFile, "file", "", "path to a .env file (required)")
	envImportCmd.Flags().BoolVar(&envSecret, "secret", false, "mark all imported vars as secrets")
	_ = envImportCmd.MarkFlagRequired("file")
	envSetCmd.ValidArgsFunction = completeApps
	envImportCmd.ValidArgsFunction = completeApps

	envCmd.AddCommand(envSetCmd, envImportCmd)
	appCmd.AddCommand(envCmd)
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage an application's environment variables",
}

var envSetCmd = &cobra.Command{
	Use:   "set [app] KEY=VALUE",
	Short: "Set a single environment variable",
	Long: "Sets one env var on the app (positional, or the app bound by `miabi use`).\n" +
		"With a bound app: `miabi apps env set KEY=VALUE`; else `miabi apps env set <app> KEY=VALUE`.",
	Example: "  miabi apps env set web LOG_LEVEL=debug\n  miabi apps env set API_KEY=secret --secret   # bound app",
	Args:    cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// The last arg is always KEY=VALUE; an optional leading arg is the app.
		appRefArg := ""
		kv := args[len(args)-1]
		if len(args) == 2 {
			appRefArg = args[0]
		}
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return fmt.Errorf("expected KEY=VALUE, got %q", kv)
		}
		ctx := context.Background()
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		ws, err := workspaceRef(ctx, c, eff)
		if err != nil {
			return err
		}
		appID, _, err := resolveAppRef(ctx, c, eff, ws, appRefArg)
		if err != nil {
			return err
		}
		if err := c.SetEnv(ctx, ws, appID, api.SetEnvRequest{Key: key, Value: value, IsSecret: envSecret}); err != nil {
			return err
		}
		ui.Success("Set %s %s", ui.Bold(key), ui.Dim("(redeploy to apply)"))
		return nil
	},
}

var envImportCmd = &cobra.Command{
	Use:     "import [app] --file .env",
	Short:   "Bulk-import environment variables from a .env file",
	Example: "  miabi apps env import web --file .env\n  miabi apps env import --file .env   # bound app",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, err := os.ReadFile(envFile)
		if err != nil {
			return err
		}
		ctx := context.Background()
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		ws, err := workspaceRef(ctx, c, eff)
		if err != nil {
			return err
		}
		appID, _, err := resolveAppRef(ctx, c, eff, ws, appArg(args))
		if err != nil {
			return err
		}
		if err := c.ImportEnv(ctx, ws, appID, api.ImportEnvRequest{Content: string(content), IsSecret: envSecret}); err != nil {
			return err
		}
		ui.Success("Imported env from %s %s", envFile, ui.Dim("(redeploy to apply)"))
		return nil
	},
}
