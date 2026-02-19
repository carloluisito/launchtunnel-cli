package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the default control plane API base URL.
const DefaultBaseURL = "https://api.launchtunnel.dev"

// APIError represents a structured error response from the control plane.
type APIError struct {
	HTTPStatus int
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// apiErrorEnvelope is the wire format for error responses.
type apiErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ---------- request/response types ----------

// CreateTunnelRequest is the body for POST /api/v1/tunnels.
type CreateTunnelRequest struct {
	Protocol    string `json:"protocol"`
	LocalPort   int    `json:"local_port"`
	LocalHost   string `json:"local_host,omitempty"`
	Name        string `json:"name,omitempty"`
	Subdomain   string `json:"subdomain,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Description string `json:"description,omitempty"`
	Branch      string `json:"branch,omitempty"`
	ExpiresIn   string `json:"expires_in,omitempty"`
}

// TunnelResponse is a single tunnel object returned by the API.
type TunnelResponse struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id,omitempty"`
	Protocol      string     `json:"protocol"`
	LocalPort     int        `json:"local_port"`
	LocalHost     string     `json:"local_host"`
	Name          string     `json:"name,omitempty"`
	Subdomain     string     `json:"subdomain"`
	AssignedPort  int        `json:"assigned_port,omitempty"`
	PublicURL     string     `json:"public_url"`
	Status        string     `json:"status"`
	RelayEndpoint string     `json:"relay_endpoint,omitempty"`
	SessionToken  string     `json:"session_token,omitempty"`
	BytesIn       int64      `json:"bytes_in"`
	BytesOut      int64      `json:"bytes_out"`
	RequestCount  int64      `json:"request_count"`
	Description   string     `json:"description,omitempty"`
	Branch        string     `json:"branch,omitempty"`
	WorkspaceID   string     `json:"workspace_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`

	ConnectionEvents []ConnectionEvent `json:"connection_events,omitempty"`
}

// ConnectionEvent is a tunnel lifecycle event.
type ConnectionEvent struct {
	Event     string    `json:"event"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Pagination holds paging metadata.
type Pagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type tunnelEnvelope struct {
	Tunnel TunnelResponse `json:"tunnel"`
}

type tunnelsEnvelope struct {
	Tunnels    []TunnelResponse `json:"tunnels"`
	Pagination Pagination       `json:"pagination"`
}

// VerifyResponse is returned by GET /api/v1/auth/verify.
type VerifyResponse struct {
	User UserInfo `json:"user"`
}

// UserInfo is the user object in verify and cli-session responses.
type UserInfo struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Tier   string `json:"tier"`
	Status string `json:"status"`
}

// CLISessionResponse is returned by GET /api/v1/auth/cli-session/{session_id}.
type CLISessionResponse struct {
	Status string `json:"status"`
	APIKey string `json:"api_key,omitempty"`
}

// CreateAPIKeyRequest is the body for POST /api/v1/api-keys.
type CreateAPIKeyRequest struct {
	Name string `json:"name,omitempty"`
}

// APIKeyResponse is a single API key object.
type APIKeyResponse struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"`
	Name       string     `json:"name,omitempty"`
	Key        string     `json:"key,omitempty"` // only on create
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type apiKeyEnvelope struct {
	APIKey APIKeyResponse `json:"api_key"`
}

type apiKeysEnvelope struct {
	APIKeys []APIKeyResponse `json:"api_keys"`
}

type deleteEnvelope struct {
	Deleted bool `json:"deleted"`
}

// ---------- Client ----------

// Client is an HTTP client for the LaunchTunnel control plane API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a new Client.
func New(baseURL, apiKey string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetAPIKey updates the API key used for authentication.
func (c *Client) SetAPIKey(key string) {
	c.apiKey = key
}

// BaseURL returns the base URL the client is configured with.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// ---------- tunnel operations ----------

// CreateTunnel creates a new tunnel.
func (c *Client) CreateTunnel(req CreateTunnelRequest) (*TunnelResponse, error) {
	var env tunnelEnvelope
	if err := c.do("POST", "/api/v1/tunnels", req, &env); err != nil {
		return nil, err
	}
	return &env.Tunnel, nil
}

// ListTunnels returns the user's tunnels.
func (c *Client) ListTunnels() ([]TunnelResponse, error) {
	var env tunnelsEnvelope
	if err := c.do("GET", "/api/v1/tunnels", nil, &env); err != nil {
		return nil, err
	}
	return env.Tunnels, nil
}

// GetTunnel returns a single tunnel by ID.
func (c *Client) GetTunnel(tunnelID string) (*TunnelResponse, error) {
	var env tunnelEnvelope
	if err := c.do("GET", "/api/v1/tunnels/"+tunnelID, nil, &env); err != nil {
		return nil, err
	}
	return &env.Tunnel, nil
}

// StopTunnel tells the control plane to mark a tunnel as stopped.
func (c *Client) StopTunnel(tunnelID string) error {
	return c.do("POST", "/api/v1/tunnels/"+tunnelID+"/stop", nil, nil)
}

// DeleteTunnel stops and deletes a tunnel.
func (c *Client) DeleteTunnel(tunnelID string) error {
	var env tunnelEnvelope
	return c.do("DELETE", "/api/v1/tunnels/"+tunnelID, nil, &env)
}

// SetTunnelPassword sets password protection on a tunnel.
func (c *Client) SetTunnelPassword(tunnelID, password string) error {
	body := map[string]string{"password": password}
	return c.do("PUT", "/api/v1/tunnels/"+tunnelID+"/password", body, nil)
}

// SetTunnelIPAllowlist sets the IP allowlist on a tunnel.
func (c *Client) SetTunnelIPAllowlist(tunnelID string, allowlist []string) error {
	body := map[string]any{"allowlist": allowlist}
	return c.do("PUT", "/api/v1/tunnels/"+tunnelID+"/ip-allowlist", body, nil)
}

// ---------- auth operations ----------

// VerifyAPIKey validates the current API key and returns user info.
func (c *Client) VerifyAPIKey() (*VerifyResponse, error) {
	var resp VerifyResponse
	if err := c.do("GET", "/api/v1/auth/verify", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PollCLISession polls the CLI session endpoint during the browser login flow.
func (c *Client) PollCLISession(sessionID string) (*CLISessionResponse, error) {
	var resp CLISessionResponse
	if err := c.doNoAuth("GET", "/api/v1/auth/cli-session/"+sessionID, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ---------- API key operations ----------

// CreateAPIKey creates a new API key.
func (c *Client) CreateAPIKey(name string) (*APIKeyResponse, error) {
	var env apiKeyEnvelope
	body := CreateAPIKeyRequest{Name: name}
	if err := c.do("POST", "/api/v1/api-keys", body, &env); err != nil {
		return nil, err
	}
	return &env.APIKey, nil
}

// ListAPIKeys returns all API keys for the user.
func (c *Client) ListAPIKeys() ([]APIKeyResponse, error) {
	var env apiKeysEnvelope
	if err := c.do("GET", "/api/v1/api-keys", nil, &env); err != nil {
		return nil, err
	}
	return env.APIKeys, nil
}

// RevokeAPIKey revokes an API key by its ID.
func (c *Client) RevokeAPIKey(keyID string) error {
	var env deleteEnvelope
	return c.do("DELETE", "/api/v1/api-keys/"+keyID, nil, &env)
}

// ---------- internal HTTP helpers ----------

func (c *Client) do(method, path string, body any, out any) error {
	return c.doReq(method, path, body, out, true)
}

func (c *Client) doNoAuth(method, path string, body any, out any) error {
	return c.doReq(method, path, body, out, false)
}

func (c *Client) doReq(method, path string, body any, out any, auth bool) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshalling request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if auth && c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to reach LaunchTunnel servers: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, data)
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

func parseAPIError(status int, body []byte) *APIError {
	var env apiErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Code != "" {
		return &APIError{
			HTTPStatus: status,
			Code:       env.Error.Code,
			Message:    env.Error.Message,
		}
	}
	return &APIError{
		HTTPStatus: status,
		Code:       "UNKNOWN_ERROR",
		Message:    fmt.Sprintf("unexpected HTTP %d", status),
	}
}
