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
