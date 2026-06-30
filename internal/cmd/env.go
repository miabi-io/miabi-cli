package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	envApp      string
	envSecret   bool
	envFile     string
)

func init() {
	envCmd.PersistentFlags().StringVar(&envApp, "app", "", "application slug or id (required)")
	_ = envCmd.MarkPersistentFlagRequired("app")

	envSetCmd.Flags().BoolVar(&envSecret, "secret", false, "store the value encrypted at rest")
	envImportCmd.Flags().StringVar(&envFile, "file", "", "path to a .env file (required)")
	envImportCmd.Flags().BoolVar(&envSecret, "secret", false, "mark all imported vars as secrets")
	_ = envImportCmd.MarkFlagRequired("file")

	envCmd.AddCommand(envSetCmd, envImportCmd)
	rootCmd.AddCommand(envCmd)
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage an application's environment variables",
}

var envSetCmd = &cobra.Command{
	Use:   "set KEY=VALUE",
	Short: "Set a single environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value, ok := strings.Cut(args[0], "=")
		if !ok || key == "" {
			return fmt.Errorf("expected KEY=VALUE, got %q", args[0])
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
		appID, err := c.ResolveAppID(ctx, ws, envApp)
		if err != nil {
			return err
		}
		if err := c.SetEnv(ctx, ws, appID, api.SetEnvRequest{Key: key, Value: value, IsSecret: envSecret}); err != nil {
			return err
		}
		fmt.Printf("Set %s (redeploy to apply)\n", key)
		return nil
	},
}

var envImportCmd = &cobra.Command{
	Use:   "import --file .env",
	Short: "Bulk-import environment variables from a .env file",
	RunE: func(cmd *cobra.Command, _ []string) error {
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
		appID, err := c.ResolveAppID(ctx, ws, envApp)
		if err != nil {
			return err
		}
		if err := c.ImportEnv(ctx, ws, appID, api.ImportEnvRequest{Content: string(content), IsSecret: envSecret}); err != nil {
			return err
		}
		fmt.Printf("Imported env from %s (redeploy to apply)\n", envFile)
		return nil
	},
}
