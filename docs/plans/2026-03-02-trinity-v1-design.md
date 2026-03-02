# Trinity v1 Design

## Overview

Trinity는 Claude Code, Gemini CLI, Copilot CLI 3개의 AI 코딩 어시스턴트를 하나의 TUI에서 통합 사용하는 도구.
각 CLI의 공식 클라이언트/SDK를 서버모드로 띄우고, Trinity TUI에서 HTTP로 통신한다.

## Architecture

```
┌─────────────────────────────────────┐
│  Trinity TUI (Go + Bubble Tea)      │
│  ┌───────┬─────────┬──────────┐     │
│  │Claude │ Gemini  │ Copilot  │ Tab │
│  └───┬───┴────┬────┴────┬─────┘     │
│      │        │         │           │
│   SSE Client (공통 HTTP 클라이언트)  │
└──────┼────────┼─────────┼───────────┘
       │        │         │
       ▼        ▼         ▼
   :3100     :3200     :3300
   Claude    Gemini    Copilot
   Agent SDK  Fork     SDK+headless
   래퍼서버   서버모드   래퍼서버
```

## Components

### 1. Server: Claude (Node.js)

- `servers/claude/server.mjs`
- `@anthropic-ai/claude-agent-sdk` 사용
- `query()` → AsyncGenerator<SDKMessage> 스트리밍
- 세션 유지: `resume: sessionId`
- 포크 불필요 (SDK 래퍼)

### 2. Server: Gemini (Node.js, Fork)

- `fullstack-hub/gemini-cli` 포크에 `--serve` 플래그 추가
- 수정 파일:
  - `packages/cli/src/config/config.ts` — `--serve`, `--port` 플래그
  - `packages/cli/src/gemini.tsx` — 서버모드 분기
  - `packages/cli/src/httpServer.ts` — 신규 HTTP 서버
- `GeminiClient.sendMessageStream()` → SSE 스트리밍
- 세션 유지: `GeminiClient` 인스턴스 재사용

### 3. Server: Copilot (Node.js)

- `servers/copilot/server.ts`
- `@github/copilot-sdk` 사용
- `copilot --headless --port 4321` 데몬 + SDK 래퍼
- `CopilotSession.send()` / `sendAndWait()` 스트리밍
- 세션 유지: `CopilotSession` 인스턴스 재사용

### 4. Trinity TUI (Go)

- Go + Bubble Tea 기반
- Tab 전환 (Claude / Gemini / Copilot)
- 입력 → 현재 탭 서버로 POST /chat (SSE)
- 명령어:
  - `/update` — upstream 동기화 + 빌드 + 서버 재시작
  - `/reset` — 현재 탭 서버 세션 초기화

## Common Server API

3개 서버 모두 동일한 인터페이스:

```
POST /chat    { "message": "..." }  → SSE stream
POST /reset   {}                    → { "status": "reset" }
GET  /health                        → { "status": "ok" }
```

### SSE Event Format

```
data: {"type":"content","delta":"텍스트 청크"}
data: {"type":"tool_call","name":"Read"}
data: {"type":"done"}
data: {"type":"error","message":"에러 메시지"}
```

## Configuration

```yaml
# config.yaml
servers:
  claude:
    url: http://localhost:3100
  gemini:
    url: http://localhost:3200
  copilot:
    url: http://localhost:3300

default_agent: claude
```

## /update Flow

```
1. 각 서버 GET /health → 현재 상태 확인
2. Gemini: git fetch upstream → 변경 있으면 merge + build + 서버 재시작
3. Claude/Copilot: npm update SDK → 변경 있으면 서버 재시작
4. 변경 없으면 스킵
```

## Project Structure

```
trinity/
├── cmd/trinity/main.go       # TUI 진입점
├── internal/
│   ├── tui/                   # Bubble Tea TUI
│   │   ├── app.go             # 메인 모델
│   │   ├── tab.go             # 탭 컴포넌트
│   │   └── input.go           # 입력 처리
│   ├── client/                # SSE HTTP 클라이언트
│   │   └── sse.go
│   ├── router/                # 에이전트 라우팅
│   │   └── router.go
│   └── updater/               # /update 로직
│       └── updater.go
├── servers/
│   ├── claude/
│   │   ├── server.mjs
│   │   └── package.json
│   └── copilot/
│       ├── server.ts
│       ├── package.json
│       └── tsconfig.json
├── config.yaml
├── go.mod
└── go.sum
```

Gemini 서버는 별도 포크 레포(`fullstack-hub/gemini-cli`)에서 관리.

## Auth

- Claude: `~/.claude/` 기존 OAuth 토큰 사용 (Agent SDK가 자동 처리)
- Gemini: `~/.gemini/` 기존 Google OAuth 사용 (CLI가 자동 처리)
- Copilot: `COPILOT_GITHUB_TOKEN` 또는 기존 `~/.copilot/` 인증 사용

## v2 Planned Features

- `/plugins:sync` — 3개 CLI 간 플러그인/스킬/MCP 양방향 동기화
- 위임(delegation) — 에이전트 간 서브태스크 위임
- 통합 플러그인 시스템 (`~/.trinity/`)
