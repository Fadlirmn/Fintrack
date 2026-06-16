package n8n

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is an HTTP client for interacting with an n8n instance.
// Supports listing workflows, triggering webhook workflows, and checking status.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	enabled bool
}

// NewClient creates a new n8n client.
// If baseURL is empty, the client is disabled.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
		enabled: baseURL != "",
	}
}

// IsEnabled reports whether n8n URL is configured.
func (c *Client) IsEnabled() bool { return c.enabled }

// ── Workflow Types ─────────────────────────────────────────────────────────────

type Workflow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type WorkflowListResponse struct {
	Data []Workflow `json:"data"`
}

type WebhookResponse struct {
	OK      bool        `json:"ok"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ── API Methods ───────────────────────────────────────────────────────────────

// ListWorkflows fetches all workflows from n8n API.
func (c *Client) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	if !c.enabled {
		return nil, fmt.Errorf("n8n not configured")
	}

	url := fmt.Sprintf("%s/api/v1/workflows", c.baseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-N8N-API-KEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("n8n list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("n8n list returned %d", resp.StatusCode)
	}

	var result WorkflowListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("n8n list decode: %w", err)
	}
	return result.Data, nil
}

// TriggerWebhook sends a POST to an n8n webhook URL (path = webhook path set in n8n).
// payload is optional; pass nil for no body.
func (c *Client) TriggerWebhook(ctx context.Context, webhookPath string, payload map[string]interface{}) (string, error) {
	if !c.enabled {
		return "", fmt.Errorf("n8n not configured")
	}

	url := fmt.Sprintf("%s/webhook/%s", c.baseURL, webhookPath)

	var bodyBytes []byte
	var err error
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("n8n webhook marshal: %w", err)
		}
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Optionally include API key for authenticated webhooks
	if c.apiKey != "" {
		req.Header.Set("X-N8N-API-KEY", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("n8n webhook: %w", err)
	}
	defer resp.Body.Close()

	// Try to decode response as JSON
	var result map[string]interface{}
	if jsonErr := json.NewDecoder(resp.Body).Decode(&result); jsonErr != nil {
		return fmt.Sprintf("Workflow triggered (status %d)", resp.StatusCode), nil
	}

	// Return a readable summary of the response
	if msg, ok := result["message"].(string); ok && msg != "" {
		return msg, nil
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

// GetWorkflowStatus checks if a specific workflow is active by ID.
func (c *Client) GetWorkflowStatus(ctx context.Context, workflowID string) (Workflow, error) {
	if !c.enabled {
		return Workflow{}, fmt.Errorf("n8n not configured")
	}

	url := fmt.Sprintf("%s/api/v1/workflows/%s", c.baseURL, workflowID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-N8N-API-KEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return Workflow{}, fmt.Errorf("n8n workflow status: %w", err)
	}
	defer resp.Body.Close()

	var wf Workflow
	_ = json.NewDecoder(resp.Body).Decode(&wf)
	return wf, nil
}
