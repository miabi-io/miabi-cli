package cmd

import (
	"context"
	"fmt"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	rollbackApp        string
	rollbackRelease    uint
	rollbackToPrevious bool
)

func init() {
	f := rollbackCmd.Flags()
	f.StringVar(&rollbackApp, "app", "", "application slug or id (required)")
	f.UintVar(&rollbackRelease, "release", 0, "release id to roll back to")
	f.BoolVar(&rollbackToPrevious, "to-previous", false, "roll back to the most recent inactive release")
	_ = rollbackCmd.MarkFlagRequired("app")
	rootCmd.AddCommand(rollbackCmd)
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback --app <slug> (--release <id> | --to-previous)",
	Short: "Roll an application back to a previous release",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := context.Background()
		if rollbackRelease == 0 && !rollbackToPrevious {
			return fmt.Errorf("specify --release <id> or --to-previous")
		}
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		ws, err := workspaceRef(ctx, c, eff)
		if err != nil {
			return err
		}
		appID, err := c.ResolveAppID(ctx, ws, rollbackApp)
		if err != nil {
			return err
		}

		target := rollbackRelease
		if rollbackToPrevious {
			rels, err := c.Releases(ctx, ws, appID)
			if err != nil {
				return err
			}
			// Releases are newest-first; the previous release is the highest-version
			// one that is not currently active.
			for _, r := range rels {
				if !r.Active {
					target = r.ID
					break
				}
			}
			if target == 0 {
				return fmt.Errorf("no previous release to roll back to")
			}
		}

		dep, err := c.Rollback(ctx, ws, appID, api.RollbackRequest{ReleaseID: target})
		if err != nil {
			return err
		}
		if flagJSON {
			return printJSON(dep)
		}
		fmt.Printf("Rollback to release %d started (deployment #%d, status: %s)\n", target, dep.ID, dep.Status)
		return nil
	},
}
