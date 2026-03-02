package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChat_StreamsEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"content\",\"delta\":\"Hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"content\",\"delta\":\" world\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"done\"}\n\n")
	}))
	defer server.Close()

	c := New(server.URL)
	ch, err := c.Chat(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	var events []SSEEvent
	for e := range ch {
		events = append(events, e)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Delta != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", events[0].Delta)
	}
	if events[2].Type != EventDone {
		t.Errorf("expected done event, got %s", events[2].Type)
	}
}

func TestHealth_ReturnsOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	c := New(server.URL)
	ok, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected health to be ok")
	}
}
