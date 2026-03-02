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
)

type SSEEvent struct {
	Type    EventType `json:"type"`
	Delta   string    `json:"delta,omitempty"`
	Name    string    `json:"name,omitempty"`
	Message string    `json:"message,omitempty"`
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

func (c *Client) Chat(ctx context.Context, message string) (<-chan SSEEvent, error) {
	body, _ := json.Marshal(map[string]string{"message": message})
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
