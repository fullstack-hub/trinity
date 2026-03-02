package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type EventType string

const (
	EventContent  EventType = "content"
	EventToolCall EventType = "tool_call"
	EventDone     EventType = "done"
	EventError    EventType = "error"
	EventModel    EventType = "model"
	EventThinking EventType = "thinking"
)

type SSEEvent struct {
	Type    EventType `json:"type"`
	Delta   string    `json:"delta,omitempty"`
	Name    string    `json:"name,omitempty"`
	Message string    `json:"message,omitempty"`
	Model   string    `json:"model,omitempty"`
	CostUSD float64   `json:"cost_usd,omitempty"`
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) Chat(ctx context.Context, message string, model string, budget int) (<-chan SSEEvent, error) {
	payload := map[string]interface{}{"message": message}
	if model != "" {
		payload["model"] = model
	}
	if budget > 0 {
		payload["budget"] = budget
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	ch := make(chan SSEEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var event SSEEvent
			if json.Unmarshal([]byte(data), &event) == nil {
				ch <- event
			}
		}
	}()
	return ch, nil
}

func (c *Client) Reset(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/reset", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// UsageResponse holds rate limit usage from the Claude /usage endpoint.
type UsageResponse struct {
	FiveHourPct    float64 `json:"five_hour_pct"`
	FiveHourResets string  `json:"five_hour_resets_at"`
	WeeklyPct      float64 `json:"weekly_pct"`
	WeeklyResets   string  `json:"weekly_resets_at"`
}

func (c *Client) Usage(ctx context.Context) (*UsageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/usage", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("usage returned %d", resp.StatusCode)
	}
	var usage UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, err
	}
	if usage.FiveHourPct == 0 && usage.WeeklyPct == 0 {
		return nil, nil
	}
	return &usage, nil
}

// CopilotQuotaResponse holds premium request quota from Copilot's account.getQuota RPC.
type CopilotQuotaResponse struct {
	RemainingPct float64 `json:"remaining_pct"`
	Used         int     `json:"used"`
	Limit        int     `json:"limit"`
	Overage      int     `json:"overage"`
	ResetDate    string  `json:"reset_date"`
	Unlimited    bool    `json:"unlimited"`
}

func (c *Client) Quota(ctx context.Context) (*CopilotQuotaResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/quota", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("quota returned %d", resp.StatusCode)
	}
	var quota CopilotQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		return nil, err
	}
	return &quota, nil
}

// GeminiStatsBucket holds per-model quota from Gemini's /stats endpoint.
type GeminiStatsBucket struct {
	ModelID          string  `json:"model_id"`
	RemainingFraction float64 `json:"remaining_fraction"`
	RemainingPct     float64 `json:"remaining_pct"`
	ResetTime        string  `json:"reset_time"`
}

// GeminiStatsResponse holds Gemini quota data from /stats endpoint.
type GeminiStatsResponse struct {
	CurrentModel string             `json:"current_model"`
	Buckets      []GeminiStatsBucket `json:"buckets"`
}

func (c *Client) Stats(ctx context.Context) (*GeminiStatsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/stats", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("stats returned %d", resp.StatusCode)
	}
	var stats GeminiStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func (c *Client) Health(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+"/health", nil)
	if err != nil {
		return false, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == 200, nil
}
