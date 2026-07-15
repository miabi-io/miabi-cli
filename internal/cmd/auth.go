package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/miabi-io/miabi-cli/internal/api"
	"github.com/miabi-io/miabi-cli/internal/config"
	"github.com/miabi-io/miabi-cli/internal/ui"
	"github.com/spf13/cobra"
)

var loginWeb bool

func init() {
	loginCmd.Flags().BoolVar(&loginWeb, "web", false, "open the browser to generate a token, then paste it back")
	rootCmd.AddCommand(loginCmd, whoamiCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Store the server URL and API token in ~/.miabi/config.yaml",
	Long: "Validates the token against GET /me and persists server+token to the config file.\n" +
		"With --web, opens the console's \"Copy login command\" page in your browser and\n" +
		"prompts you to paste the generated token. CI should set MIABI_SERVER/MIABI_TOKEN.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if loginWeb {
			return runWebLogin()
		}
		c, eff, err := newClient()
		if err != nil {
			return err
		}
		me, err := c.Me(context.Background())
		if err != nil {
			return fmt.Errorf("token rejected: %w", err)
		}
		f, err := config.Load()
		if err != nil {
			return err
		}
		name := loginContextName(f, flagContext, eff.URL)
		ctx := f.EnsureContext(name)
		// Persist the server connection (URL + TLS trust), the token, and the
		// identity (for offline display) into this context, and make it current.
		ctx.Server = config.Server{URL: eff.URL, CA: eff.CA, InsecureSkip: eff.InsecureSkip}
		ctx.Token = eff.Token
		ctx.User = &config.Identity{Name: me.Name, Username: me.Username, Email: me.Email}
		if err := config.Save(f); err != nil {
			return err
		}
		who := me.Name
		if me.Username != "" {
			who = fmt.Sprintf("%s (@%s)", me.Name, me.Username)
		}
		fmt.Printf("Logged in as %s <%s> at %s\n", who, me.Email, eff.URL)
		fmt.Printf("Context %q saved to %s\n", name, config.Path())
		return nil
	},
}

// runWebLogin implements `miabi login --web`: it opens the console's token page
// in the browser and reads the pasted token from stdin, then validates and saves
// it — so a user never has to hand-copy `--token` from the display page.
func runWebLogin() error {
	url := strings.TrimRight(firstNonEmpty(serverFlag(), os.Getenv("MIABI_SERVER"), os.Getenv("MIABI_URL")), "/")
	if url == "" {
		return fmt.Errorf("no server URL configured — pass --server or set MIABI_SERVER")
	}
	tokenPage := url + "/request-token"
	fmt.Printf("Opening %s\n", tokenPage)
	if err := openBrowser(tokenPage); err != nil {
		fmt.Printf("Could not open a browser automatically — visit the URL above.\n")
	}
	fmt.Print("Paste your token: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return fmt.Errorf("no token entered")
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return fmt.Errorf("no token entered")
	}

	c, err := api.New(api.Options{BaseURL: url, Token: token, CAFile: flagCA, InsecureSkip: flagInsecure, Verbose: flagVerbose})
	if err != nil {
		return err
	}
	me, err := c.Me(context.Background())
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	f, err := config.Load()
	if err != nil {
		return err
	}
	name := loginContextName(f, flagContext, url)
	ctx := f.EnsureContext(name)
	ctx.Server = config.Server{URL: url, CA: flagCA, InsecureSkip: flagInsecure}
	ctx.Token = token
	ctx.User = &config.Identity{Name: me.Name, Username: me.Username, Email: me.Email}
	if err := config.Save(f); err != nil {
		return err
	}
	who := me.Name
	if me.Username != "" {
		who = fmt.Sprintf("%s (@%s)", me.Name, me.Username)
	}
	fmt.Printf("Logged in as %s <%s> at %s\n", who, me.Email, url)
	fmt.Printf("Context %q saved to %s\n", name, config.Path())
	return nil
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
