package cmd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/miabi-io/miabi-cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	loginWeb       bool
	loginNoBrowser bool
)

func init() {
	loginCmd.Flags().BoolVar(&loginWeb, "web", false, "force the browser sign-in flow, even if MIABI_TOKEN is set")
	loginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "print the token page URL and paste the token back (no local callback)")
	rootCmd.AddCommand(loginCmd, whoamiCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in and store the server URL and API token in ~/.miabi/config.yaml",
	Long: "By default, opens your browser to sign in (password or SSO) and captures the\n" +
		"minted token automatically via a one-time local callback — no copy-paste.\n\n" +
		"With --token/MIABI_TOKEN set, validates that token against GET /me and saves it\n" +
		"(use this in CI). With --no-browser, prints the token page URL and reads a pasted\n" +
		"token instead — for machines that can't open a local callback.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// An explicit token (flag or env) is the non-interactive path: validate and
		// save it directly, no browser. --web overrides to force the browser flow.
		if !loginWeb && firstNonEmpty(flagToken, os.Getenv("MIABI_TOKEN")) != "" {
			return runTokenLogin()
		}
		serverURL, err := resolveLoginServer()
		if err != nil {
			return err
		}
		if loginNoBrowser {
			return runManualLogin(serverURL)
		}
		return runLoopbackLogin(serverURL)
	},
}

// resolveLoginServer picks the panel URL to sign in against: an explicit
// --server/env, else the current context's server so a bare `miabi login`
// re-authenticates the context in place.
func resolveLoginServer() (string, error) {
	if u := firstNonEmpty(serverFlag(), os.Getenv("MIABI_SERVER"), os.Getenv("MIABI_URL")); u != "" {
		return strings.TrimRight(u, "/"), nil
	}
	if f, err := config.Load(); err == nil {
		if cur := f.CurrentContext(); cur != nil && cur.Server.URL != "" {
			return strings.TrimRight(cur.Server.URL, "/"), nil
		}
	}
	return "", fmt.Errorf("no server URL configured — pass --server or set MIABI_SERVER")
}

// runTokenLogin is the non-interactive path: validate the --token/env token
// against /me and persist it.
func runTokenLogin() error {
	c, eff, err := newClient()
	if err != nil {
		return err
	}
	me, err := c.Me(context.Background())
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	return saveLogin(eff.URL, eff.Token, config.Server{URL: eff.URL, CA: eff.CA, InsecureSkip: eff.InsecureSkip}, me)
}

// runLoopbackLogin is the default browser sign-in. It starts a local server on a
// random loopback port, opens the console's /cli/authorize page pointed at that
// callback, and waits for the browser to hand back a single-use code — which it
// exchanges for the token. The token never touches the clipboard or the terminal.
func runLoopbackLogin(serverURL string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("could not start the local login server: %w", err)
	}
	defer func() { _ = ln.Close() }()
	callback := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	state, err := randomState()
	if err != nil {
		return err
	}

	authorizeURL := serverURL + "/cli/authorize?redirect_uri=" + url.QueryEscape(callback) + "&state=" + url.QueryEscape(state)

	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("error") != "":
			writeCallbackPage(w, false)
			resCh <- result{err: fmt.Errorf("sign-in failed: %s", q.Get("error"))}
		case q.Get("state") != state:
			writeCallbackPage(w, false)
			resCh <- result{err: errors.New("state mismatch — aborting for safety")}
		case q.Get("code") == "":
			writeCallbackPage(w, false)
			resCh <- result{err: errors.New("no code returned from the browser")}
		default:
			writeCallbackPage(w, true)
			resCh <- result{code: q.Get("code")}
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	fmt.Printf("Opening your browser to sign in:\n  %s\n\n", authorizeURL)
	if err := openBrowser(authorizeURL); err != nil {
		fmt.Println("Could not open a browser automatically — visit the URL above.")
	}
	fmt.Println("Waiting for the browser to finish sign-in… (Ctrl-C to cancel)")

	var code string
	select {
	case res := <-resCh:
		if res.err != nil {
			return res.err
		}
		code = res.code
	case <-time.After(5 * time.Minute):
		return errors.New("timed out waiting for the browser sign-in")
	}

	c, err := api.New(api.Options{BaseURL: serverURL, CAFile: flagCA, InsecureSkip: flagInsecure, Verbose: flagVerbose})
	if err != nil {
		return err
	}
	tok, err := c.ClaimLoginToken(context.Background(), code)
	if err != nil {
		return fmt.Errorf("could not retrieve the login token: %w", err)
	}
	return persistBrowserLogin(serverURL, tok.Token)
}

// runManualLogin is the --no-browser fallback: it opens (or prints) the console's
// "Copy login command" page and reads a pasted token from stdin. For machines
// that can reach a browser elsewhere but can't accept a local callback.
func runManualLogin(serverURL string) error {
	tokenPage := serverURL + "/request-token"
	fmt.Printf("Open this page, sign in, and copy the token:\n  %s\n", tokenPage)
	_ = openBrowser(tokenPage)
	fmt.Print("Paste your token: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return fmt.Errorf("no token entered")
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return fmt.Errorf("no token entered")
	}
	return persistBrowserLogin(serverURL, token)
}

// persistBrowserLogin validates a token minted by a browser flow against /me and
// saves the context, honoring the current --certificate-authority/--insecure trust.
func persistBrowserLogin(serverURL, token string) error {
	c, err := api.New(api.Options{BaseURL: serverURL, Token: token, CAFile: flagCA, InsecureSkip: flagInsecure, Verbose: flagVerbose})
	if err != nil {
		return err
	}
	me, err := c.Me(context.Background())
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	return saveLogin(serverURL, token, config.Server{URL: serverURL, CA: flagCA, InsecureSkip: flagInsecure}, me)
}

// saveLogin writes the server, token, and identity into the resolved context and
// makes it current, then prints the signed-in summary. Shared by every login path.
func saveLogin(serverURL, token string, server config.Server, me *api.Me) error {
	f, err := config.Load()
	if err != nil {
		return err
	}
	name := loginContextName(f, flagContext, serverURL)
	ctx := f.EnsureContext(name)
	ctx.Server = server
	ctx.Token = token
	ctx.User = &config.Identity{Name: me.Name, Username: me.Username, Email: me.Email}
	if err := config.Save(f); err != nil {
		return err
	}
	who := me.Name
	if me.Username != "" {
		who = fmt.Sprintf("%s (@%s)", me.Name, me.Username)
	}
	fmt.Printf("Logged in as %s <%s> at %s\n", who, me.Email, serverURL)
	fmt.Printf("Context %q saved to %s\n", name, config.Path())
	return nil
}

// randomState returns a URL-safe, unguessable value binding the browser round-trip
// to this CLI process (CSRF protection for the loopback callback).
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate login state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// writeCallbackPage renders the tiny page the browser lands on after the redirect
// back to the loopback callback.
func writeCallbackPage(w http.ResponseWriter, success bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := "You can close this window and return to your terminal."
	if !success {
		msg = "Sign-in failed. Return to your terminal and try again."
	}
	fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>Miabi CLI</title>"+
		"<body style=\"font-family:system-ui,sans-serif;text-align:center;padding:3rem;color:#334155\">"+
		"<h2>Miabi CLI</h2><p>%s</p></body>", msg)
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// openBrowser best-effort opens url in the platform's default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, append(args, url)...).Start()
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated identity, scopes, and bound workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, _, err := newClient()
		if err != nil {
			return err
		}
		me, err := c.Me(context.Background())
		if err != nil {
			return err
		}
		if structured() {
			return emit(me)
		}
		fmt.Printf("%s   %s <%s>\n", ui.Dim("User: "), me.Name, me.Email)
		if me.Username != "" {
			fmt.Printf("%s   @%s\n", ui.Dim("Name: "), me.Username)
		}
		fmt.Printf("%s   %s\n", ui.Dim("Auth: "), me.Auth.Method)
		if len(me.Auth.Scopes) > 0 {
			fmt.Printf("%s %s\n", ui.Dim("Scopes:"), strings.Join(me.Auth.Scopes, ", "))
		}
		// Local context (name + workspace + bound app) from the config file.
		if f, ferr := config.Load(); ferr == nil {
			if f.Current != "" {
				fmt.Printf("%s %s\n", ui.Dim("Context:"), f.Current)
			}
			if cur := f.CurrentContext(); cur != nil {
				if cur.Workspace != nil {
					fmt.Printf("%s    %s\n", ui.Dim("Space:"), cur.Workspace.Name)
				}
				if cur.App != nil {
					fmt.Printf("%s      %s\n", ui.Dim("App:"), cur.App.Name)
				}
			}
		}
		return nil
	},
}

// loginContextName picks the context name to write on login: the explicit
// --context, else the current context when re-logging into the same server (so a
// bare `miabi login` refreshes it in place), else the server's hostname, else
// "default".
func loginContextName(f *config.File, flagContext, serverURL string) string {
	if flagContext != "" {
		return flagContext
	}
	if cur := f.CurrentContext(); cur != nil && cur.Server.URL == serverURL {
		return f.Current
	}
	if u, err := url.Parse(serverURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "default"
}
