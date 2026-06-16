package fintrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is an HTTP client for calling the FinTrack API's internal endpoints.
// All requests carry the shared API key in the X-API-Key header.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient creates a new FinTrack internal client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ── Binding ───────────────────────────────────────────────────────────────────

type BindingResponse struct {
	Linked bool   `json:"linked"`
	UserID string `json:"user_id"`
}

// GetBinding checks whether a Telegram chat_id is linked to a FinTrack account.
func (c *Client) GetBinding(ctx context.Context, chatID string) (BindingResponse, error) {
	url := fmt.Sprintf("%s/internal/v1/binding?chat_id=%s", c.baseURL, chatID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return BindingResponse{}, fmt.Errorf("fintrack binding: %w", err)
	}
	defer resp.Body.Close()

	var result BindingResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── Link Account ──────────────────────────────────────────────────────────────

type LinkRequest struct {
	ChatID string `json:"chat_id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

type LinkResponse struct {
	OK     bool   `json:"ok"`
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Error  string `json:"error,omitempty"`
}

// LinkAccount sends a link-code verification request to FinTrack.
func (c *Client) LinkAccount(ctx context.Context, chatID, code, name string) (LinkResponse, error) {
	body, _ := json.Marshal(LinkRequest{ChatID: chatID, Code: code, Name: name})
	url := fmt.Sprintf("%s/internal/v1/link", c.baseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return LinkResponse{}, fmt.Errorf("fintrack link: %w", err)
	}
	defer resp.Body.Close()

	var result LinkResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return result, fmt.Errorf("fintrack link failed: %s", result.Error)
	}
	return result, nil
}

// ── Balance ───────────────────────────────────────────────────────────────────

type BalanceResponse struct {
	DailyBudget    int64 `json:"daily_budget"`
	WeeklyBudget   int64 `json:"weekly_budget"`
	MonthlyBudget  int64 `json:"monthly_budget"`
	FixedDaily     int64 `json:"fixed_daily"`
	SpendableToday int64 `json:"spendable_today"`
	SpendableWeek  int64 `json:"spendable_week"`
	SpendableMonth int64 `json:"spendable_month"`
}

// GetBalance returns spendable balance data for a user.
func (c *Client) GetBalance(ctx context.Context, userID string) (BalanceResponse, error) {
	url := fmt.Sprintf("%s/internal/v1/balance?user_id=%s", c.baseURL, userID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return BalanceResponse{}, fmt.Errorf("fintrack balance: %w", err)
	}
	defer resp.Body.Close()

	var result BalanceResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── Summary ───────────────────────────────────────────────────────────────────

type CategoryEntry struct {
	Category string `json:"category"`
	Total    int64  `json:"total"`
}

type SummaryResponse struct {
	Month          string          `json:"month"`
	Year           int             `json:"year"`
	Total          int64           `json:"total"`
	TopCategories  []CategoryEntry `json:"top_categories"`
}

// GetSummary returns the monthly spending summary for a user.
func (c *Client) GetSummary(ctx context.Context, userID string) (SummaryResponse, error) {
	url := fmt.Sprintf("%s/internal/v1/summary?user_id=%s", c.baseURL, userID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return SummaryResponse{}, fmt.Errorf("fintrack summary: %w", err)
	}
	defer resp.Body.Close()

	var result SummaryResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

// ── Save Transaction ──────────────────────────────────────────────────────────

type SaveTxRequest struct {
	UserID      string `json:"user_id"`
	Category    string `json:"category"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
}

type SaveTxResponse struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	Category    string `json:"category"`
	Amount      int64  `json:"amount"`
	Description string `json:"description"`
	Error       string `json:"error,omitempty"`
}

// SaveTransaction saves a new transaction via the FinTrack internal API.
func (c *Client) SaveTransaction(ctx context.Context, userID, description, category string, amount int64) (SaveTxResponse, error) {
	body, _ := json.Marshal(SaveTxRequest{
		UserID: userID, Category: category, Amount: amount, Description: description,
	})
	url := fmt.Sprintf("%s/internal/v1/transactions", c.baseURL)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return SaveTxResponse{}, fmt.Errorf("fintrack save tx: %w", err)
	}
	defer resp.Body.Close()

	var result SaveTxResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusCreated {
		return result, fmt.Errorf("fintrack save tx failed: %s", result.Error)
	}
	return result, nil
}
