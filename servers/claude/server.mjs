import { createServer } from "node:http";
import https from "node:https";
import { execFileSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { query } from "@anthropic-ai/claude-agent-sdk";

const PORT = parseInt(process.env.PORT ?? "3100", 10);
const sessions = new Map();

// ── OAuth Usage API (same approach as oh-my-claude-sisyphus) ──

function getOAuthCredentials() {
  // macOS Keychain first
  if (process.platform === "darwin") {
    try {
      const result = execFileSync(
        "/usr/bin/security",
        ["find-generic-password", "-s", "Claude Code-credentials", "-w"],
        { encoding: "utf-8", timeout: 2000, stdio: ["pipe", "pipe", "pipe"] }
      ).trim();
      if (result) {
        const parsed = JSON.parse(result);
        const creds = parsed.claudeAiOauth || parsed;
        if (creds.accessToken) return creds.accessToken;
      }
    } catch {}
  }
  // File fallback
  try {
    const credPath = join(homedir(), ".claude/.credentials.json");
    if (!existsSync(credPath)) return null;
    const parsed = JSON.parse(readFileSync(credPath, "utf-8"));
    const creds = parsed.claudeAiOauth || parsed;
    if (creds.accessToken) return creds.accessToken;
  } catch {}
  return null;
}

function fetchAnthropicUsage(accessToken) {
  return new Promise((resolve) => {
    const req = https.request({
      hostname: "api.anthropic.com",
      path: "/api/oauth/usage",
      method: "GET",
      headers: {
        Authorization: `Bearer ${accessToken}`,
        "anthropic-beta": "oauth-2025-04-20",
        "Content-Type": "application/json",
      },
      timeout: 10000,
    }, (res) => {
      let data = "";
      res.on("data", (chunk) => { data += chunk; });
      res.on("end", () => {
        if (res.statusCode === 200) {
          try { resolve(JSON.parse(data)); } catch { resolve(null); }
        } else { resolve(null); }
      });
    });
    req.on("error", () => resolve(null));
    req.on("timeout", () => { req.destroy(); resolve(null); });
    req.end();
  });
}

// Cache usage for 30s
let usageCache = null;
let usageCacheTime = 0;

async function getCachedUsage() {
  if (usageCache && Date.now() - usageCacheTime < 30000) return usageCache;
  const token = getOAuthCredentials();
  if (!token) return null;
  const raw = await fetchAnthropicUsage(token);
  if (!raw) return null;
  const fiveHour = raw.five_hour?.utilization;
  const sevenDay = raw.seven_day?.utilization;
  if (fiveHour == null && sevenDay == null) return null;
  const clamp = (v) => v == null || !isFinite(v) ? 0 : Math.max(0, Math.min(100, v));
  usageCache = {
    five_hour_pct: clamp(fiveHour),
    five_hour_resets_at: raw.five_hour?.resets_at || null,
    weekly_pct: clamp(sevenDay),
    weekly_resets_at: raw.seven_day?.resets_at || null,
  };
  usageCacheTime = Date.now();
  return usageCache;
}

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

  if (method === "GET" && url === "/usage") {
    const usage = await getCachedUsage();
    if (usage) {
      return send(res, 200, usage);
    }
    return send(res, 200, { error: "no credentials" });
  }

  if (method === "POST" && url === "/reset") {
    sessions.clear();
    return send(res, 200, { status: "reset" });
  }

  if (method === "POST" && url === "/chat") {
    let body;
    try { body = await readBody(req); }
    catch { return send(res, 400, { error: "Invalid JSON" }); }

    const { message, session_id, model: requestedModel, budget } = body;
    if (!message) return send(res, 400, { error: "message required" });

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
    });

    const resumeId = session_id && sessions.has(session_id)
      ? sessions.get(session_id) : undefined;

    try {
      const queryOptions = {
        ...(resumeId ? { resume: resumeId } : {}),
        permissionMode: "bypassPermissions",
        allowedTools: ["Read", "Edit", "Bash", "Glob", "Grep", "Write"],
        maxTurns: 10,
      };
      if (requestedModel) {
        queryOptions.model = requestedModel;
      }
      if (budget && budget > 0) {
        queryOptions.thinking = { type: "enabled", budget_tokens: budget };
      }

      const agentQuery = query({
        prompt: message,
        options: queryOptions,
      });

      let modelSent = false;
      for await (const msg of agentQuery) {
        if (msg.type === "system" && msg.session_id) {
          sessions.set(msg.session_id, msg.session_id);
          res.write(`data: ${JSON.stringify({ type: "session", session_id: msg.session_id })}\n\n`);
        } else if (msg.type === "assistant") {
          // Extract model from the first assistant message
          if (!modelSent && msg.message?.model) {
            res.write(`data: ${JSON.stringify({ type: "model", model: msg.message.model })}\n\n`);
            modelSent = true;
          }
          for (const block of msg.message?.content ?? []) {
            if (block.type === "thinking") {
              res.write(`data: ${JSON.stringify({ type: "thinking", delta: block.thinking })}\n\n`);
            } else if (block.type === "text") {
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
