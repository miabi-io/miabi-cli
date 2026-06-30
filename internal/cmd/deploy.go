package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	deployApp      string
	deployTag      string
	deployStrategy string
	deployWait     bool
	deployTimeout  time.Duration
)

func init() {
	f := deployCmd.Flags()
	f.StringVar(&deployApp, "app", "", "application slug or id (required)")
	f.StringVar(&deployTag, "tag", "", "image tag to deploy (e.g. the git SHA)")
	f.StringVar(&deployStrategy, "strategy", "", "deploy strategy: recreate | rolling | canary")
	f.BoolVar(&deployWait, "wait", false, "block until the deployment is terminal; non-zero exit on failure")
	f.DurationVar(&deployTimeout, "timeout", 10*time.Minute, "max time to wait with --wait")
	_ = deployCmd.MarkFlagRequired("app")
	rootCmd.AddCommand(deployCmd)
}

var deployCmd = &cobra.Command{
	Use:   "deploy --app <slug> [--tag <tag>] [--wait]",
	Short: "Deploy an application (optionally waiting for the result)",
	Long: "Triggers a deployment of the app. With --tag, deploys that image tag (the\n" +
		"common CI flow). With --wait, blocks until the deployment finishes and exits\n" +
		"non-zero if it failed — suitable as a CI gate.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := context.Background()
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		ws, err := workspaceRef(ctx, c, eff)
		if err != nil {
			return err
		}
		appID, err := c.ResolveAppID(ctx, ws, deployApp)
		if err != nil {
			return err
		}

		dep, err := c.Deploy(ctx, ws, appID, api.DeployRequest{Tag: deployTag, Strategy: deployStrategy})
		if err != nil {
			return err
		}
		if !deployWait {
			if flagJSON {
				return printJSON(dep)
			}
			fmt.Printf("Deployment #%d started (status: %s)\n", dep.ID, dep.Status)
			return nil
		}

		if !flagJSON {
			fmt.Printf("Deployment #%d started; waiting…\n", dep.ID)
		}
		wctx, cancel := context.WithTimeout(ctx, deployTimeout)
		defer cancel()
		onUpdate := func(status string) {
			if !flagJSON {
				fmt.Printf("  → %s\n", status)
			}
		}
		final, err := c.WaitForDeploy(wctx, ws, appID, dep.ID, onUpdate)
		if err != nil {
			return fmt.Errorf("waiting for deployment #%d: %w", dep.ID, err)
		}
		if flagJSON {
			_ = printJSON(final)
		}
		if api.IsFailure(final.Status) {
			if final.Error != "" {
				fmt.Fprintf(os.Stderr, "Deployment #%d failed: %s\n", final.ID, final.Error)
			}
			// Non-zero exit so CI fails the step.
			return fmt.Errorf("deployment #%d failed", final.ID)
		}
		if !flagJSON {
			fmt.Printf("Deployment #%d succeeded.\n", final.ID)
		}
		return nil
	},
}
