package home

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is an HTTP client for calling the Home Server service.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	enabled bool // false if HOME_SERVER_URL is not configured
}

// NewClient creates a new Home Server client.
// If baseURL is empty, the client is disabled and all calls return a friendly error.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
		enabled: baseURL != "",
	}
}

// IsEnabled reports whether a home-server URL has been configured.
func (c *Client) IsEnabled() bool { return c.enabled }

// ── Status ────────────────────────────────────────────────────────────────────

type StatusResponse struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Uptime   string `json:"uptime"`
	LoadAvg  string `json:"load_avg"`
}

// GetStatus fetches basic server info from the home-server.
func (c *Client) GetStatus(ctx context.Context) (StatusResponse, error) {
	if !c.enabled {
		return StatusResponse{}, fmt.Errorf("home server not configured")
	}
	return doGet[StatusResponse](ctx, c, "/status")
}

// ── Resources ─────────────────────────────────────────────────────────────────

type ResourcesResponse struct {
	CPUPercent  float64 `json:"cpu_percent"`
	RAMUsed     int64   `json:"ram_used_mb"`
	RAMTotal    int64   `json:"ram_total_mb"`
	DiskUsed    int64   `json:"disk_used_gb"`
	DiskTotal   int64   `json:"disk_total_gb"`
}

// GetResources fetches CPU, RAM, and disk usage from the home-server.
func (c *Client) GetResources(ctx context.Context) (ResourcesResponse, error) {
	if !c.enabled {
		return ResourcesResponse{}, fmt.Errorf("home server not configured")
	}
	return doGet[ResourcesResponse](ctx, c, "/resources")
}

// ── Devices ───────────────────────────────────────────────────────────────────

type Device struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	MAC      string `json:"mac"`
}

type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

// GetDevices returns devices discovered on the local network.
func (c *Client) GetDevices(ctx context.Context) (DevicesResponse, error) {
	if !c.enabled {
		return DevicesResponse{}, fmt.Errorf("home server not configured")
	}
	return doGet[DevicesResponse](ctx, c, "/devices")
}

// ── PC Control ────────────────────────────────────────────────────────────────

type PCActionResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// PCSleep sends a suspend command to the home server.
func (c *Client) PCAction(ctx context.Context, action string) (PCActionResponse, error) {
	if !c.enabled {
		return PCActionResponse{}, fmt.Errorf("home server not configured")
	}
	url := fmt.Sprintf("%s/pc/%s", c.baseURL, action)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return PCActionResponse{}, fmt.Errorf("home server pc/%s: %w", action, err)
	}
	defer resp.Body.Close()

	var result PCActionResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── Script Runner ─────────────────────────────────────────────────────────────

type RunScriptRequest struct {
	Script string `json:"script"`
}

type RunScriptResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// RunScript asks the home-server to execute a whitelisted script by name.
func (c *Client) RunScript(ctx context.Context, scriptName string) (RunScriptResponse, error) {
	if !c.enabled {
		return RunScriptResponse{}, fmt.Errorf("home server not configured")
	}

	body, _ := json.Marshal(RunScriptRequest{Script: scriptName})
	url := fmt.Sprintf("%s/scripts/run", c.baseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytesReader(body))
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return RunScriptResponse{}, fmt.Errorf("home server run script: %w", err)
	}
	defer resp.Body.Close()

	var result RunScriptResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func doGet[T any](ctx context.Context, c *Client, path string) (T, error) {
	var zero T
	url := fmt.Sprintf("%s%s", c.baseURL, path)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, fmt.Errorf("home server %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("home server %s returned %d", path, resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, fmt.Errorf("home server %s decode: %w", path, err)
	}
	return result, nil
}

func bytesReader(b []byte) *bytesReadCloser { return &bytesReadCloser{b: b, pos: 0} }

type bytesReadCloser struct{ b []byte; pos int }

func (r *bytesReadCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.b) {
		return 0, fmt.Errorf("EOF")
	}
	n = copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}
func (r *bytesReadCloser) Close() error { return nil }
