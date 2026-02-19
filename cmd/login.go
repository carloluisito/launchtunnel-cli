package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/carloluisito/launchtunnel-cli/client"
	"github.com/carloluisito/launchtunnel-cli/config"
	"github.com/spf13/cobra"
)

const (
	browserPollInterval = 2 * time.Second
	browserPollTimeout  = 5 * time.Minute
)

func newLoginCmd() *cobra.Command {
	var apiKeyFlag string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate the CLI with a LaunchTunnel account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := client.New(cliCfg.APIURL, "")

			if apiKeyFlag != "" {
				return loginWithAPIKey(c, apiKeyFlag)
			}
			return loginWithBrowser(c)
		},
	}

	cmd.Flags().StringVar(&apiKeyFlag, "api-key", "", "authenticate directly with an API key")
	return cmd
}

func loginWithAPIKey(c *client.Client, key string) error {
	c.SetAPIKey(key)
	resp, err := c.VerifyAPIKey()
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.HTTPStatus == 401 {
			fmt.Fprintln(os.Stderr, "Invalid API key. Check your key at https://app.launchtunnel.dev/settings/api-keys")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Unable to reach LaunchTunnel servers. Check your internet connection.")
		os.Exit(1)
	}

	if err := config.SaveCredentials(&config.Credentials{
		APIKey: key,
		APIURL: cliCfg.APIURL,
		Email:  resp.User.Email,
	}); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	fmt.Printf("Authenticated as %s. API key stored.\n", resp.User.Email)
	return nil
}

func loginWithBrowser(c *client.Client) error {
	sessionID := generateSessionID()
	authURL := fmt.Sprintf("%s/cli?session=%s", cliCfg.FrontendURL, sessionID)

	fmt.Println("Opening browser for authentication...")
	fmt.Printf("If the browser does not open, visit: %s\n", authURL)

	openBrowser(authURL)

	deadline := time.Now().Add(browserPollTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(browserPollInterval)

		resp, err := c.PollCLISession(sessionID)
		if err != nil {
			continue
		}

		if resp.Status == "authenticated" && resp.APIKey != "" {
			c.SetAPIKey(resp.APIKey)
			verify, err := c.VerifyAPIKey()
			email := ""
			if err == nil {
				email = verify.User.Email
			}

			if err := config.SaveCredentials(&config.Credentials{
				APIKey: resp.APIKey,
				APIURL: cliCfg.APIURL,
				Email:  email,
			}); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}

			if email != "" {
				fmt.Printf("Authenticated as %s. API key stored.\n", email)
			} else {
				fmt.Println("Authenticated. API key stored.")
			}
			return nil
		}
	}

	fmt.Fprintln(os.Stderr, "Login timed out. Run 'lt login' to try again.")
	os.Exit(1)
	return nil
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
