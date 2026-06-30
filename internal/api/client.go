// Package api is a thin wrapper over the Miabi /api/v1 HTTP surface, built on
// github.com/jkaninda/okapi/client. It unwraps the server's {success,data,error}
// envelope and maps the structured error to a Go error with a stable code.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jkaninda/okapi/client"
)

// Version is stamped at build time (see .goreleaser.yaml); used in User-Agent.
var Version = "dev"

// Client talks to one panel with one token.
type Client struct {
	c       *client.Client
	verbose bool
}

// New builds a client for baseURL authenticating with token.
func New(baseURL, token string, verbose bool) *Client {
	opts := []client.Option{
		client.WithBearerToken(token),
		client.WithUserAgent("miabi-cli/" + Version),
		client.WithTimeout(30 * time.Second),
		// Safe to retry: every call below is a GET or an idempotent action.
		client.WithRetry(client.RetryPolicy{MaxAttempts: 3, BaseDelay: 200 * time.Millisecond, MaxDelay: 2 * time.Second}),
	}
	if verbose {
		opts = append(opts, client.WithMiddleware(client.LoggingMiddleware(os.Stderr)))
	}
	return &Client{c: client.New(baseURL, opts...), verbose: verbose}
}

// APIError is the server's structured error envelope.
type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Detail     string `json:"error"`
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = e.Detail
	}
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, msg)
	}
	return msg
}

// envelope is the standard response wrapper.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *APIError       `json:"error"`
}

// do performs a request and decodes the envelope's data into out (out may be
// nil). On a non-2xx it returns the server's *APIError when present.
func (c *Client) do(rb *client.RequestBuilder, out any) error {
	resp, err := rb.Do()
	if err != nil {
		// Transport-level or non-2xx (okapi returns *client.HTTPError). Try to
		// surface the structured envelope from the body.
		if he, ok := err.(*client.HTTPError); ok {
			var env envelope
			if json.Unmarshal(he.Body, &env) == nil && env.Error != nil {
				return env.Error
			}
			return fmt.Errorf("HTTP %d: %s", he.StatusCode, string(he.Body))
		}
		return err
	}
	if out == nil {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if env.Error != nil {
		return env.Error
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(c.c.Get(path).WithContext(ctx), out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	rb := c.c.Post(path).WithContext(ctx)
	if body != nil {
		rb = rb.JSONBody(body)
	}
	return c.do(rb, out)
}

func (c *Client) put(ctx context.Context, path string, body, out any) error {
	rb := c.c.Put(path).WithContext(ctx)
	if body != nil {
		rb = rb.JSONBody(body)
	}
	return c.do(rb, out)
}

// --- identity & workspaces -------------------------------------------------

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var m Me
	return &m, c.get(ctx, "/api/v1/me", &m)
}

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var ws []Workspace
	return ws, c.get(ctx, "/api/v1/workspaces", &ws)
}

// ResolveWorkspaceName turns a user-supplied workspace reference into the
// canonical name (handle) the API addresses by. ref may be the name, a UID, or a
// numeric id (resolved to its name for back-compat); an empty ref falls back to
// fallback (the persisted/bound workspace) and finally to the sole workspace the
// caller can see. The returned value is always a name, since scoped routes are
// addressed by name.
func (c *Client) ResolveWorkspaceName(ctx context.Context, ref, fallback string) (string, error) {
	if ref == "" {
		ref = fallback
	}
	if ref == "" {
		ws, err := c.Workspaces(ctx)
		if err != nil {
			return "", err
		}
		if len(ws) == 1 {
			return ws[0].Name, nil
		}
		return "", fmt.Errorf("no workspace selected — pass --workspace, or run `miabi workspace switch <name>`")
	}
	ws, err := c.Workspaces(ctx)
	if err != nil {
		return "", err
	}
	idMatch, _ := strconv.ParseUint(ref, 10, 64)
	for _, w := range ws {
		if w.Name == ref || w.UID == ref || (idMatch != 0 && w.ID == uint(idMatch)) {
			return w.Name, nil
		}
	}
	return "", fmt.Errorf("workspace %q not found among your workspaces", ref)
}

// --- applications ----------------------------------------------------------

func (c *Client) Apps(ctx context.Context, ws string) ([]App, error) {
	var apps []App
	return apps, c.get(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps", ws), &apps)
}

func (c *Client) App(ctx context.Context, ws string, appID uint) (*App, error) {
	var a App
	return &a, c.get(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d", ws, appID), &a)
}

// ResolveAppID turns an app slug (or numeric id) into the numeric id paths use.
func (c *Client) ResolveAppID(ctx context.Context, ws string, ref string) (uint, error) {
	if id, err := strconv.ParseUint(ref, 10, 64); err == nil {
		return uint(id), nil
	}
	apps, err := c.Apps(ctx, ws)
	if err != nil {
		return 0, err
	}
	for _, a := range apps {
		if a.Slug == ref {
			return a.ID, nil
		}
	}
	return 0, fmt.Errorf("application %q not found in this workspace", ref)
}

// --- deploy / rollback / releases -----------------------------------------

func (c *Client) Deploy(ctx context.Context, ws string, appID uint, req DeployRequest) (*Deployment, error) {
	var d Deployment
	return &d, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/deploy", ws, appID), req, &d)
}

func (c *Client) Rollback(ctx context.Context, ws string, appID uint, req RollbackRequest) (*Deployment, error) {
	var d Deployment
	return &d, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/rollback", ws, appID), req, &d)
}

func (c *Client) Deployments(ctx context.Context, ws string, appID uint) ([]Deployment, error) {
	var deps []Deployment
	return deps, c.get(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/deployments", ws, appID), &deps)
}

// Deployment resolves a single deployment. The API exposes no per-deployment GET
// route, so it is found within the (recent) deployments list.
func (c *Client) Deployment(ctx context.Context, ws string, appID, depID uint) (*Deployment, error) {
	deps, err := c.Deployments(ctx, ws, appID)
	if err != nil {
		return nil, err
	}
	for i := range deps {
		if deps[i].ID == depID {
			return &deps[i], nil
		}
	}
	return nil, fmt.Errorf("deployment #%d not found", depID)
}

func (c *Client) Releases(ctx context.Context, ws string, appID uint) ([]Release, error) {
	var rs []Release
	return rs, c.get(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/releases", ws, appID), &rs)
}

// --- env -------------------------------------------------------------------

func (c *Client) SetEnv(ctx context.Context, ws string, appID uint, req SetEnvRequest) error {
	return c.put(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/env", ws, appID), req, nil)
}

func (c *Client) ImportEnv(ctx context.Context, ws string, appID uint, req ImportEnvRequest) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apps/%d/env/import", ws, appID), req, nil)
}

// ========= declarative apply -----------------------------------------------------

// PlanApply previews converging the workspace to the manifest bundle (dry-run).
func (c *Client) PlanApply(ctx context.Context, ws string, manifests string, prune bool) (*Plan, error) {
	var p Plan
	req := ApplyRequest{Manifests: manifests, Prune: prune, DryRun: true}
	return &p, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apply", ws), req, &p)
}

// Apply converges the workspace to the manifest bundle.
func (c *Client) Apply(ctx context.Context, ws string, manifests string, prune bool) (*ApplyResult, error) {
	var r ApplyResult
	req := ApplyRequest{Manifests: manifests, Prune: prune, DryRun: false}
	return &r, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apply", ws), req, &r)
}

// PlanDelete previews deleting exactly the resources the bundle names (dry-run).
func (c *Client) PlanDelete(ctx context.Context, ws string, manifests string) (*Plan, error) {
	var p Plan
	req := ApplyRequest{Manifests: manifests, Delete: true, DryRun: true}
	return &p, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apply", ws), req, &p)
}

// Delete removes exactly the resources the bundle names.
func (c *Client) Delete(ctx context.Context, ws string, manifests string) (*ApplyResult, error) {
	var r ApplyResult
	req := ApplyRequest{Manifests: manifests, Delete: true, DryRun: false}
	return &r, c.post(ctx, fmt.Sprintf("/api/v1/workspaces/%s/apply", ws), req, &r)
}

// WaitForDeploy polls a deployment until it reaches a terminal state or the
// context is cancelled/times out, calling onUpdate (if non-nil) on each status
// change. It returns the final deployment.
func (c *Client) WaitForDeploy(ctx context.Context, ws string, appID, depID uint, onUpdate func(status string)) (*Deployment, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	last := ""
	for {
		d, err := c.Deployment(ctx, ws, appID, depID)
		if err != nil {
			return nil, err
		}
		if d.Status != last {
			last = d.Status
			if onUpdate != nil {
				onUpdate(d.Status)
			}
		}
		if IsTerminal(d.Status) {
			return d, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
