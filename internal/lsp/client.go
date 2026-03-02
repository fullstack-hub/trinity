package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientState represents the current state of an LSP client.
type ClientState int

const (
	StateStarting ClientState = iota
	StateRunning
	StateStopped
	StateError
)

// Client manages a single LSP server process.
type Client struct {
	Config ServerConfig
	State  ClientState
	Error  string // error message if State == StateError

	cmd     *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	mu      sync.Mutex
	nextID  int
	pending map[int]chan json.RawMessage
	caps    ServerCaps
	rootURI string

	// Diagnostics received from server
	diagMu      sync.Mutex
	diagnostics map[string][]Diagnostic // uri -> diagnostics
}

// NewClient creates a new LSP client for the given config and workspace.
func NewClient(cfg ServerConfig, workspace string) *Client {
	return &Client{
		Config:      cfg,
		State:       StateStopped,
		pending:     make(map[int]chan json.RawMessage),
		diagnostics: make(map[string][]Diagnostic),
		rootURI:     fileURI(workspace),
	}
}

// Start launches the LSP server process and initializes the connection.
func (c *Client) Start() error {
	c.State = StateStarting

	cmd := exec.Command(c.Config.Command, c.Config.Args...)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.State = StateError
		c.Error = err.Error()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.State = StateError
		c.Error = err.Error()
		return err
	}

	if err := cmd.Start(); err != nil {
		c.State = StateError
		c.Error = err.Error()
		return err
	}

	c.cmd = cmd
	c.stdin = stdin
	c.reader = bufio.NewReaderSize(stdout, 1024*1024)

	// Start reading responses
	go c.readLoop()

	// Send initialize (no lock held, so Stop() won't deadlock)
	if err := c.initialize(); err != nil {
		// Kill the process directly, don't call Stop() which tries TryLock
		if c.stdin != nil {
			c.stdin.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}
		c.State = StateError
		c.Error = err.Error()
		return err
	}

	c.State = StateRunning
	return nil
}

// Stop gracefully shuts down the LSP server.
func (c *Client) Stop() {
	// Use TryLock to avoid deadlock if Start() is still running
	locked := c.mu.TryLock()
	if !locked {
		// Force kill if we can't get the lock
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
		c.State = StateStopped
		return
	}
	defer c.mu.Unlock()

	if c.cmd != nil && c.cmd.Process != nil {
		// Try exit notification (best effort, don't wait for shutdown response)
		if c.stdin != nil {
			c.sendNotification("exit", nil)
		}

		done := make(chan struct{})
		go func() {
			c.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			c.cmd.Process.Kill()
		}
	}
	if c.stdin != nil {
		c.stdin.Close()
	}
	c.State = StateStopped

	// Unblock any pending requests
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *Client) initialize() error {
	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   c.rootURI,
		Capabilities: ClientCaps{
			TextDocument: &TextDocumentClientCaps{
				Hover:      &HoverCaps{ContentFormat: []string{"markdown", "plaintext"}},
				Definition: &DefinitionCaps{},
				References: &ReferencesCaps{},
			},
		},
	}

	raw, err := c.request("initialize", params, 5*time.Second)
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}
	c.caps = result.Capabilities

	// Send initialized notification
	return c.sendNotification("initialized", json.RawMessage("{}"))
}

// Hover returns hover information at the given position.
func (c *Client) Hover(fileURI string, line, char int) (*HoverResult, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI},
		Position:     Position{Line: line, Character: char},
	}
	raw, err := c.request("textDocument/hover", params, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var result HoverResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Definition returns the definition location(s) for the symbol at the given position.
func (c *Client) Definition(fileURI string, line, char int) ([]Location, error) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI},
		Position:     Position{Line: line, Character: char},
	}
	raw, err := c.request("textDocument/definition", params, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	// Can be Location or []Location
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		var single Location
		if err2 := json.Unmarshal(raw, &single); err2 != nil {
			return nil, err
		}
		return []Location{single}, nil
	}
	return locs, nil
}

// References returns all references to the symbol at the given position.
func (c *Client) References(fileURI string, line, char int) ([]Location, error) {
	params := ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI},
		Position:     Position{Line: line, Character: char},
		Context:      ReferenceContext{IncludeDeclaration: true},
	}
	raw, err := c.request("textDocument/references", params, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var locs []Location
	if err := json.Unmarshal(raw, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// DocumentSymbols returns the symbols in the given file.
func (c *Client) DocumentSymbols(fileURI string) (json.RawMessage, error) {
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: fileURI},
	}
	return c.request("textDocument/documentSymbol", params, 5*time.Second)
}

// DidOpen notifies the server that a file was opened.
func (c *Client) DidOpen(fileURI, languageID, text string) error {
	params := struct {
		TextDocument TextDocumentItem `json:"textDocument"`
	}{
		TextDocument: TextDocumentItem{
			URI:        fileURI,
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	}
	return c.sendNotification("textDocument/didOpen", params)
}

// GetDiagnostics returns the latest diagnostics for a file URI.
func (c *Client) GetDiagnostics(uri string) []Diagnostic {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	return c.diagnostics[uri]
}

// --- Internal ---

func (c *Client) request(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch

	data, _ := json.Marshal(params)
	msg := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  data,
	}
	if err := c.writeMessage(msg); err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case result, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("connection closed")
		}
		return result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("request timeout: %s", method)
	}
}

func (c *Client) sendNotification(method string, params any) error {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = data
	}
	msg := jsonrpcNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}
	return c.writeMessage(msg)
}

func (c *Client) writeMessage(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	_, err = fmt.Fprint(c.stdin, header+string(body))
	return err
}

func (c *Client) readLoop() {
	for {
		body, err := c.readMessage()
		if err != nil {
			return
		}

		var resp jsonrpcResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}

		// Server notification (e.g. textDocument/publishDiagnostics)
		if resp.Method != "" {
			c.handleNotification(resp.Method, resp.Params)
			continue
		}

		// Response to a request
		if resp.ID != nil {
			c.mu.Lock()
			ch, ok := c.pending[*resp.ID]
			if ok {
				delete(c.pending, *resp.ID)
			}
			c.mu.Unlock()
			if ok {
				if resp.Error != nil {
					ch <- nil
				} else {
					ch <- resp.Result
				}
			}
		}
	}
}

func (c *Client) readMessage() ([]byte, error) {
	// Read headers
	contentLen := 0
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLen, _ = strconv.Atoi(val)
		}
	}
	if contentLen == 0 {
		return nil, fmt.Errorf("invalid content length")
	}

	body := make([]byte, contentLen)
	_, err := io.ReadFull(c.reader, body)
	return body, err
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		var diag PublishDiagnosticsParams
		if json.Unmarshal(params, &diag) == nil {
			c.diagMu.Lock()
			c.diagnostics[diag.URI] = diag.Diagnostics
			c.diagMu.Unlock()
		}
	}
}

func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + url.PathEscape(abs)
}

// FileURI converts a file path to a file:// URI (exported for use by other packages).
func FileURI(path string) string {
	return fileURI(path)
}
