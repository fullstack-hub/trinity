# Trinity v1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 3개 AI CLI(Claude Code, Gemini CLI, Copilot CLI)를 하나의 TUI에서 탭 전환하며 사용하는 통합 도구 구현

**Architecture:** 각 CLI를 HTTP 서버모드로 띄우고 (Claude/Copilot은 SDK 래퍼, Gemini는 포크 수정), Go+Bubble Tea TUI에서 SSE로 통신

**Tech Stack:** Go 1.26, Bubble Tea, Node.js 22, TypeScript, @anthropic-ai/claude-agent-sdk, @github/copilot-sdk

---

## Task 1: Trinity Go 프로젝트 초기화

**Files:**
- Create: `cmd/trinity/main.go`
- Create: `go.mod`
- Create: `config.yaml`

**Step 1: Go 모듈 초기화**

```bash
cd /Users/jaden.krust/Documents/GitHub/fullstackhub/trinity
go mod init github.com/fullstack-hub/trinity
```

**Step 2: Bubble Tea 의존성 추가**

```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/bubbles@latest
go get gopkg.in/yaml.v3@latest
```

**Step 3: config.yaml 작성**

```yaml
servers:
  claude:
    url: http://localhost:3100
  gemini:
    url: http://localhost:3200
  copilot:
    url: http://localhost:3300

default_agent: claude
```

**Step 4: main.go 스켈레톤 작성**

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func initialModel() tea.Model {
	return &app{activeTab: 0}
}

type app struct {
	activeTab int
}

func (a *app) Init() tea.Cmd { return nil }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return a, tea.Quit
		case "tab":
			a.activeTab = (a.activeTab + 1) % 3
		}
	}
	return a, nil
}

func (a *app) View() string {
	tabs := []string{"Claude", "Gemini", "Copilot"}
	return fmt.Sprintf("[ %s ] | [ %s ] | [ %s ]\n\nActive: %s\n\nPress Tab to switch, q to quit",
		tabs[0], tabs[1], tabs[2], tabs[a.activeTab])
}
```

**Step 5: 빌드 및 실행 테스트**

Run: `go build -o trinity ./cmd/trinity && ./trinity`
Expected: TUI 화면 표시, Tab으로 전환, q로 종료

**Step 6: Commit**

```bash
git add go.mod go.sum cmd/ config.yaml
git commit -m "feat: trinity Go project init with Bubble Tea skeleton"
```

---

## Task 2: SSE HTTP 클라이언트 구현

**Files:**
- Create: `internal/client/sse.go`
- Create: `internal/client/sse_test.go`

**Step 1: SSE 이벤트 타입 정의 및 클라이언트 작성**

```go
// internal/client/sse.go
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

// Chat sends a message and streams SSE events back via channel
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

// Reset resets the server session
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

// Health checks if server is alive
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
```

**Step 2: 테스트 작성**

```go
// internal/client/sse_test.go
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
```

**Step 3: 테스트 실행**

Run: `go test ./internal/client/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/client/
git commit -m "feat: SSE HTTP client with streaming support"
```

---

## Task 3: Config 로더 구현

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: config 패키지 작성**

```go
// internal/config/config.go
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	URL string `yaml:"url"`
}

type Config struct {
	Servers      map[string]ServerConfig `yaml:"servers"`
	DefaultAgent string                  `yaml:"default_agent"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
```

**Step 2: 테스트 작성**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
servers:
  claude:
    url: http://localhost:3100
  gemini:
    url: http://localhost:3200
default_agent: claude
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Servers["claude"].URL != "http://localhost:3100" {
		t.Errorf("unexpected claude url: %s", cfg.Servers["claude"].URL)
	}
	if cfg.DefaultAgent != "claude" {
		t.Errorf("unexpected default: %s", cfg.DefaultAgent)
	}
}
```

**Step 3: 테스트 실행**

Run: `go test ./internal/config/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/config/
git commit -m "feat: YAML config loader"
```

---

## Task 4: TUI 구현 (탭 전환 + 입력 + 스트리밍 출력)

**Files:**
- Create: `internal/tui/app.go`
- Create: `internal/tui/tab.go`
- Modify: `cmd/trinity/main.go`

**Step 1: 탭 컴포넌트 작성**

```go
// internal/tui/tab.go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	activeTabStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.HiddenBorder()).
		Padding(0, 2)
)

type TabBar struct {
	Tabs      []string
	ActiveTab int
}

func (t TabBar) View() string {
	var rendered []string
	for i, tab := range t.Tabs {
		if i == t.ActiveTab {
			rendered = append(rendered, activeTabStyle.Render(tab))
		} else {
			rendered = append(rendered, inactiveTabStyle.Render(tab))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...) + "\n" + strings.Repeat("─", 60)
}
```

**Step 2: 메인 TUI 모델 작성**

```go
// internal/tui/app.go
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fullstack-hub/trinity/internal/client"
	"github.com/fullstack-hub/trinity/internal/config"
)

type streamEventMsg client.SSEEvent
type streamErrMsg struct{ err error }

type App struct {
	cfg       *config.Config
	tabs      TabBar
	clients   map[string]*client.Client
	input     textarea.Model
	outputs   map[string]string // 탭별 출력 버퍼
	streaming bool
	cancel    context.CancelFunc
}

func NewApp(cfg *config.Config) *App {
	ta := textarea.New()
	ta.Placeholder = "메시지를 입력하세요..."
	ta.Focus()
	ta.SetHeight(3)
	ta.SetWidth(58)

	tabNames := []string{"Claude", "Gemini", "Copilot"}
	tabKeys := []string{"claude", "gemini", "copilot"}

	clients := make(map[string]*client.Client)
	for _, key := range tabKeys {
		if srv, ok := cfg.Servers[key]; ok {
			clients[key] = client.New(srv.URL)
		}
	}

	outputs := make(map[string]string)
	for _, key := range tabKeys {
		outputs[key] = ""
	}

	defaultTab := 0
	for i, key := range tabKeys {
		if key == cfg.DefaultAgent {
			defaultTab = i
		}
	}

	return &App{
		cfg:     cfg,
		tabs:    TabBar{Tabs: tabNames, ActiveTab: defaultTab},
		clients: clients,
		input:   ta,
		outputs: outputs,
	}
}

func (a *App) activeKey() string {
	keys := []string{"claude", "gemini", "copilot"}
	return keys[a.tabs.ActiveTab]
}

func (a *App) Init() tea.Cmd {
	return textarea.Blink
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if a.cancel != nil {
				a.cancel()
			}
			return a, tea.Quit
		case "tab":
			if !a.streaming {
				a.tabs.ActiveTab = (a.tabs.ActiveTab + 1) % len(a.tabs.Tabs)
			}
			return a, nil
		case "enter":
			if a.streaming {
				return a, nil
			}
			text := strings.TrimSpace(a.input.Value())
			if text == "" {
				return a, nil
			}
			// 슬래시 커맨드 처리
			if strings.HasPrefix(text, "/reset") {
				a.input.Reset()
				return a, a.handleReset()
			}
			a.input.Reset()
			a.streaming = true
			return a, a.handleChat(text)
		}

	case streamEventMsg:
		event := client.SSEEvent(msg)
		key := a.activeKey()
		switch event.Type {
		case client.EventContent:
			a.outputs[key] += event.Delta
		case client.EventToolCall:
			a.outputs[key] += fmt.Sprintf("\n[tool: %s]\n", event.Name)
		case client.EventDone:
			a.outputs[key] += "\n"
			a.streaming = false
		case client.EventError:
			a.outputs[key] += fmt.Sprintf("\n[error: %s]\n", event.Message)
			a.streaming = false
		}
		return a, nil

	case streamErrMsg:
		a.outputs[a.activeKey()] += fmt.Sprintf("\n[connection error: %s]\n", msg.err)
		a.streaming = false
		return a, nil
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a *App) handleChat(text string) tea.Cmd {
	return func() tea.Msg {
		key := a.activeKey()
		c, ok := a.clients[key]
		if !ok {
			return streamErrMsg{fmt.Errorf("no server configured for %s", key)}
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.cancel = cancel
		ch, err := c.Chat(ctx, text)
		if err != nil {
			return streamErrMsg{err}
		}
		// 첫 이벤트만 반환, 나머지는 후속 Cmd로
		for event := range ch {
			return streamEventMsg(event)
		}
		return streamEventMsg(client.SSEEvent{Type: client.EventDone})
	}
}

func (a *App) handleReset() tea.Cmd {
	return func() tea.Msg {
		key := a.activeKey()
		c, ok := a.clients[key]
		if !ok {
			return nil
		}
		c.Reset(context.Background())
		a.outputs[key] = "[session reset]\n"
		return nil
	}
}

func (a *App) View() string {
	key := a.activeKey()
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	var status string
	if a.streaming {
		status = statusStyle.Render("● streaming...")
	} else {
		status = statusStyle.Render("○ ready")
	}

	return fmt.Sprintf(
		"%s\n%s\n\n%s\n\n%s\n\n%s",
		a.tabs.View(),
		status,
		a.outputs[key],
		a.input.View(),
		statusStyle.Render("Tab: switch | Enter: send | /reset: clear | Ctrl+C: quit"),
	)
}
```

**Step 3: main.go 업데이트**

```go
// cmd/trinity/main.go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fullstack-hub/trinity/internal/config"
	"github.com/fullstack-hub/trinity/internal/tui"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(tui.NewApp(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 4: 빌드 테스트**

Run: `go build ./cmd/trinity`
Expected: 빌드 성공

**Step 5: Commit**

```bash
git add internal/tui/ cmd/trinity/
git commit -m "feat: TUI with tab switching, input, and SSE streaming"
```

---

## Task 5: Claude 서버 래퍼 구현

**Files:**
- Create: `servers/claude/server.mjs`
- Create: `servers/claude/package.json`

**Step 1: package.json 작성**

```json
{
  "name": "trinity-claude-server",
  "version": "1.0.0",
  "type": "module",
  "scripts": {
    "start": "node server.mjs"
  },
  "dependencies": {
    "@anthropic-ai/claude-agent-sdk": "^0.2.63"
  }
}
```

**Step 2: server.mjs 작성**

```javascript
import { createServer } from "node:http";
import { query } from "@anthropic-ai/claude-agent-sdk";

const PORT = parseInt(process.env.PORT ?? "3100", 10);
const sessions = new Map();

function readBody(req) {
  return new Promise((resolve, reject) => {
    let data = "";
    req.on("data", (chunk) => (data += chunk));
    req.on("end", () => {
      try { resolve(JSON.parse(data || "{}")); }
      catch { reject(new Error("Invalid JSON")); }
    });
    req.on("error", reject);
  });
}

function send(res, status, body) {
  const payload = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(payload);
}

const server = createServer(async (req, res) => {
  const url = req.url ?? "/";
  const method = req.method ?? "GET";

  if (method === "GET" && url === "/health") {
    return send(res, 200, { status: "ok", sessions: sessions.size });
  }

  if (method === "POST" && url === "/reset") {
    sessions.clear();
    return send(res, 200, { status: "reset" });
  }

  if (method === "POST" && url === "/chat") {
    let body;
    try { body = await readBody(req); }
    catch { return send(res, 400, { error: "Invalid JSON" }); }

    const { message, session_id } = body;
    if (!message) return send(res, 400, { error: "message required" });

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
    });

    const resumeId = session_id && sessions.has(session_id)
      ? sessions.get(session_id) : undefined;

    try {
      const agentQuery = query({
        prompt: message,
        options: {
          ...(resumeId ? { resume: resumeId } : {}),
          permissionMode: "bypassPermissions",
          allowedTools: ["Read", "Edit", "Bash", "Glob", "Grep", "Write"],
          maxTurns: 10,
        },
      });

      for await (const msg of agentQuery) {
        if (msg.type === "system" && msg.session_id) {
          sessions.set(msg.session_id, msg.session_id);
          res.write(`data: ${JSON.stringify({ type: "session", session_id: msg.session_id })}\n\n`);
        } else if (msg.type === "assistant") {
          for (const block of msg.message?.content ?? []) {
            if (block.type === "text") {
              res.write(`data: ${JSON.stringify({ type: "content", delta: block.text })}\n\n`);
            } else if (block.type === "tool_use") {
              res.write(`data: ${JSON.stringify({ type: "tool_call", name: block.name })}\n\n`);
            }
          }
        } else if (msg.type === "result") {
          res.write(`data: ${JSON.stringify({ type: "done", cost_usd: msg.total_cost_usd })}\n\n`);
        }
      }
    } catch (err) {
      res.write(`data: ${JSON.stringify({ type: "error", message: err.message })}\n\n`);
    }
    res.end();
    return;
  }

  send(res, 404, { error: "Not found" });
});

server.listen(PORT, () => {
  console.log(`Claude server listening on http://localhost:${PORT}`);
});
```

**Step 3: 의존성 설치 및 실행 테스트**

Run: `cd servers/claude && npm install && node server.mjs`
Expected: `Claude server listening on http://localhost:3100`

**Step 4: Health 엔드포인트 테스트**

Run: `curl http://localhost:3100/health`
Expected: `{"status":"ok","sessions":0}`

**Step 5: Commit**

```bash
git add servers/claude/
git commit -m "feat: Claude Agent SDK HTTP server wrapper"
```

---

## Task 6: Copilot 서버 래퍼 구현

**Files:**
- Create: `servers/copilot/server.ts`
- Create: `servers/copilot/package.json`
- Create: `servers/copilot/tsconfig.json`

**Step 1: package.json 작성**

```json
{
  "name": "trinity-copilot-server",
  "version": "1.0.0",
  "type": "module",
  "scripts": {
    "build": "tsc",
    "start": "node dist/server.js",
    "dev": "tsx server.ts"
  },
  "dependencies": {
    "@github/copilot-sdk": "^0.1.29"
  },
  "devDependencies": {
    "@types/node": "^22.0.0",
    "tsx": "^4.0.0",
    "typescript": "^5.5.0"
  }
}
```

**Step 2: tsconfig.json 작성**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "outDir": "dist",
    "strict": true,
    "esModuleInterop": true
  },
  "include": ["server.ts"]
}
```

**Step 3: server.ts 작성**

```typescript
import { createServer, IncomingMessage, ServerResponse } from "node:http";
import { CopilotClient, CopilotSession, approveAll } from "@github/copilot-sdk";

const PORT = parseInt(process.env.PORT ?? "3300", 10);
const CLI_URL = process.env.CLI_URL;

let copilotClient: CopilotClient | null = null;
let session: CopilotSession | null = null;

async function getSession(): Promise<CopilotSession> {
  if (!copilotClient) {
    copilotClient = new CopilotClient(CLI_URL ? { cliUrl: CLI_URL } : {});
    await copilotClient.start();
  }
  if (!session) {
    session = await copilotClient.createSession({
      onPermissionRequest: approveAll,
    });
  }
  return session;
}

async function resetSession(): Promise<void> {
  if (session) { await session.destroy().catch(() => {}); session = null; }
  if (copilotClient) { await copilotClient.stop().catch(() => {}); copilotClient = null; }
}

function readBody(req: IncomingMessage): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    let data = "";
    req.on("data", (chunk: Buffer) => (data += chunk));
    req.on("end", () => {
      try { resolve(JSON.parse(data || "{}")); }
      catch { reject(new Error("Invalid JSON")); }
    });
    req.on("error", reject);
  });
}

function send(res: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(payload);
}

const server = createServer(async (req: IncomingMessage, res: ServerResponse) => {
  const url = req.url ?? "/";
  const method = req.method ?? "GET";

  if (method === "GET" && url === "/health") {
    return send(res, 200, { status: "ok", sessionActive: session !== null });
  }

  if (method === "POST" && url === "/reset") {
    await resetSession();
    return send(res, 200, { status: "reset" });
  }

  if (method === "POST" && url === "/chat") {
    let body: Record<string, unknown>;
    try { body = await readBody(req); }
    catch { return send(res, 400, { error: "Invalid JSON" }); }

    const message = body.message as string;
    if (!message) return send(res, 400, { error: "message required" });

    const stream = (body.stream as boolean) !== false;

    let copilotSession: CopilotSession;
    try { copilotSession = await getSession(); }
    catch (err) { return send(res, 503, { error: `CLI connect failed: ${err}` }); }

    if (stream) {
      res.writeHead(200, {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        "Connection": "keep-alive",
      });

      const unsub = copilotSession.on((event) => {
        if (event.type === "assistant.message_delta") {
          res.write(`data: ${JSON.stringify({ type: "content", delta: (event as any).data?.deltaContent })}\n\n`);
        } else if (event.type === "session.idle") {
          res.write(`data: ${JSON.stringify({ type: "done" })}\n\n`);
          unsub();
          res.end();
        } else if (event.type === "session.error") {
          res.write(`data: ${JSON.stringify({ type: "error", message: String((event as any).data) })}\n\n`);
          unsub();
          res.end();
        }
      });

      copilotSession.send({ prompt: message }).catch((err: unknown) => {
        res.write(`data: ${JSON.stringify({ type: "error", message: String(err) })}\n\n`);
        unsub();
        res.end();
      });
    } else {
      try {
        const reply = await copilotSession.sendAndWait({ prompt: message });
        if (reply?.data?.content) {
          send(res, 200, { response: reply.data.content });
        } else {
          send(res, 502, { error: "No content" });
        }
      } catch (err) {
        send(res, 500, { error: String(err) });
      }
    }
    return;
  }

  send(res, 404, { error: "Not found" });
});

server.listen(PORT, () => {
  console.log(`Copilot server listening on http://localhost:${PORT}`);
});

process.on("SIGTERM", async () => { await resetSession(); process.exit(0); });
```

**Step 4: 의존성 설치 및 실행 테스트**

Run: `cd servers/copilot && npm install && npx tsx server.ts`
Expected: `Copilot server listening on http://localhost:3300`

**Step 5: Commit**

```bash
git add servers/copilot/
git commit -m "feat: Copilot SDK HTTP server wrapper"
```

---

## Task 7: Gemini CLI 포크에 서버모드 추가

**Files (in fullstack-hub/gemini-cli repo):**
- Modify: `packages/cli/src/config/config.ts`
- Modify: `packages/cli/src/gemini.tsx`
- Create: `packages/cli/src/httpServer.ts`

**Step 1: gemini-cli 포크 클론**

```bash
gh repo clone fullstack-hub/gemini-cli /Users/jaden.krust/Documents/GitHub/fullstack-hub/gemini-cli
cd /Users/jaden.krust/Documents/GitHub/fullstack-hub/gemini-cli
```

**Step 2: config.ts에 --serve, --port 플래그 추가**

`packages/cli/src/config/config.ts`의 yargs options에 추가:
```typescript
serve: {
  alias: 'S',
  type: 'boolean',
  description: 'Start an HTTP server instead of interactive mode',
  default: false,
},
port: {
  type: 'number',
  description: 'Port for --serve mode',
  default: 3200,
},
```

**Step 3: gemini.tsx에 서버모드 분기 추가**

`packages/cli/src/gemini.tsx`의 라우팅 블록에 서버모드 분기 삽입:
```typescript
if (args.serve) {
  const { startHttpServer } = await import('./httpServer.js');
  await startHttpServer({ config, settings, port: args.port ?? 3200 });
} else if (!isNonInteractive) {
  // 기존 interactive UI
} else {
  // 기존 non-interactive
}
```

**Step 4: httpServer.ts 작성**

에이전트 분석에서 제공된 전체 구현 코드를 `packages/cli/src/httpServer.ts`에 작성.
핵심: `GeminiClient.sendMessageStream()` → SSE 스트리밍, `Scheduler`로 도구 호출 처리.
(전체 코드는 설계 문서 참조)

**Step 5: 빌드 테스트**

```bash
cd packages/cli
npm run build
```
Expected: 빌드 성공

**Step 6: 서버모드 실행 테스트**

Run: `node dist/index.js --serve --port 3200`
Expected: `gemini-cli HTTP server listening on port 3200`

**Step 7: Commit & Push**

```bash
git add packages/cli/src/config/config.ts packages/cli/src/gemini.tsx packages/cli/src/httpServer.ts
git commit -m "feat: add --serve HTTP server mode"
git push origin main
```

---

## Task 8: /update 명령어 구현

**Files:**
- Create: `internal/updater/updater.go`

**Step 1: updater 패키지 작성**

```go
// internal/updater/updater.go
package updater

import (
	"fmt"
	"os/exec"
	"strings"
)

type UpdateResult struct {
	Name    string
	Updated bool
	Message string
}

func UpdateGemini(repoPath string) UpdateResult {
	// git fetch upstream
	cmd := exec.Command("git", "-C", repoPath, "fetch", "upstream")
	if err := cmd.Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("fetch failed: %v", err)}
	}

	// check if behind
	out, _ := exec.Command("git", "-C", repoPath, "rev-list", "--count", "HEAD..upstream/main").Output()
	count := strings.TrimSpace(string(out))
	if count == "0" {
		return UpdateResult{Name: "gemini", Updated: false, Message: "already up to date"}
	}

	// merge
	if err := exec.Command("git", "-C", repoPath, "merge", "upstream/main").Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("merge failed: %v", err)}
	}

	// rebuild
	if err := exec.Command("npm", "run", "build", "--prefix", repoPath+"/packages/cli").Run(); err != nil {
		return UpdateResult{Name: "gemini", Updated: false, Message: fmt.Sprintf("build failed: %v", err)}
	}

	return UpdateResult{Name: "gemini", Updated: true, Message: fmt.Sprintf("%s commits merged and rebuilt", count)}
}

func UpdateSDKServer(serverPath string, name string) UpdateResult {
	out, _ := exec.Command("npm", "outdated", "--json", "--prefix", serverPath).Output()
	if len(out) == 0 || string(out) == "{}\n" {
		return UpdateResult{Name: name, Updated: false, Message: "already up to date"}
	}

	if err := exec.Command("npm", "update", "--prefix", serverPath).Run(); err != nil {
		return UpdateResult{Name: name, Updated: false, Message: fmt.Sprintf("update failed: %v", err)}
	}

	return UpdateResult{Name: name, Updated: true, Message: "SDK updated"}
}
```

**Step 2: Commit**

```bash
git add internal/updater/
git commit -m "feat: /update command - upstream sync and SDK update"
```

---

## Task 9: 통합 테스트 및 최종 빌드

**Step 1: 전체 Go 테스트 실행**

Run: `go test ./... -v`
Expected: 모든 테스트 PASS

**Step 2: 최종 바이너리 빌드**

Run: `go build -o trinity ./cmd/trinity`
Expected: `trinity` 바이너리 생성

**Step 3: 서버 3개 동시 실행**

```bash
# 터미널 1: Claude
cd servers/claude && node server.mjs

# 터미널 2: Gemini
cd /path/to/gemini-cli && node packages/cli/dist/index.js --serve --port 3200

# 터미널 3: Copilot
cd servers/copilot && npx tsx server.ts

# 터미널 4: Trinity TUI
./trinity
```

**Step 4: 수동 검증**

- Tab으로 3개 탭 전환 확인
- 각 탭에서 메시지 전송 → 스트리밍 응답 확인
- `/reset` 동작 확인

**Step 5: 최종 Commit**

```bash
git add .
git commit -m "feat: Trinity v1 - unified AI CLI with 3 server backends"
```

---

## Task Summary

| Task | Component | Estimated Complexity |
|------|-----------|---------------------|
| 1 | Go 프로젝트 초기화 | Low |
| 2 | SSE HTTP 클라이언트 | Low |
| 3 | Config 로더 | Low |
| 4 | TUI (탭+입력+스트리밍) | Medium |
| 5 | Claude 서버 래퍼 | Low |
| 6 | Copilot 서버 래퍼 | Low |
| 7 | Gemini 포크 서버모드 | Medium |
| 8 | /update 명령어 | Low |
| 9 | 통합 테스트 | Low |
