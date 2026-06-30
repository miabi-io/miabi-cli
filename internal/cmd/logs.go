package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/spf13/cobra"
)

var (
	logsApp        string
	logsDeployment uint
	logsFollow     bool
)

func init() {
	f := logsCmd.Flags()
	f.StringVar(&logsApp, "app", "", "application slug or id (required)")
	f.UintVar(&logsDeployment, "deployment", 0, "deployment id (required)")
	f.BoolVar(&logsFollow, "follow", false, "stream logs until the deployment finishes")
	_ = logsCmd.MarkFlagRequired("app")
	_ = logsCmd.MarkFlagRequired("deployment")
	rootCmd.AddCommand(logsCmd)
}

var logsCmd = &cobra.Command{
	Use:   "logs --app <slug> --deployment <id> [--follow]",
	Short: "Show (or follow) a deployment's build/deploy logs",
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
		appID, err := c.ResolveAppID(ctx, ws, logsApp)
		if err != nil {
			return err
		}
		// The logs endpoint is a Server-Sent Events stream; stream it with the
		// standard client (okapi reads full bodies, which would defeat --follow).
		url := fmt.Sprintf("%s/api/v1/workspaces/%s/apps/%d/deployments/%d/logs",
			strings.TrimRight(eff.URL, "/"), ws, appID, logsDeployment)
		return streamSSE(ctx, url, eff.Token)
	},
}

// sseEvent mirrors the server's stream payload: {"Type":"log|status","Data":"…"}.
type sseEvent struct {
	Type string `json:"Type"`
	Data string `json:"Data"`
}

// streamSSE reads the deployment-log SSE stream, printing log lines and exiting
// when the deployment reaches a terminal status (failed → non-zero exit).
func streamSSE(ctx context.Context, url, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("logs stream: HTTP %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		var ev sseEvent
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "log":
			fmt.Println(ev.Data)
		case "status":
			fmt.Printf("[status] %s\n", ev.Data)
			if api.IsFailure(ev.Data) {
				return fmt.Errorf("deployment failed")
			}
			if api.IsTerminal(ev.Data) {
				return nil
			}
		}
	}
	return sc.Err()
}
