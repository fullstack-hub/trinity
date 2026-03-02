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

  if (method === "GET" && url === "/quota") {
    try {
      await getSession(); // ensure client is started
      const quota = await copilotClient!.rpc.account.getQuota();
      const premium = quota?.quotaSnapshots?.premium_interactions;
      if (premium) {
        return send(res, 200, {
          remaining_pct: premium.remainingPercentage ?? 0,
          used: premium.usedRequests ?? 0,
          limit: premium.entitlementRequests ?? 0,
          overage: premium.overage ?? 0,
          reset_date: premium.resetDate ?? "",
          unlimited: false,
        });
      }
      return send(res, 200, { error: "no quota data" });
    } catch (err) {
      return send(res, 200, { error: String(err) });
    }
  }

  if (method === "POST" && url === "/chat") {
    let body: Record<string, unknown>;
    try { body = await readBody(req); }
    catch { return send(res, 400, { error: "Invalid JSON" }); }

    const message = body.message as string;
    if (!message) return send(res, 400, { error: "message required" });

    let copilotSession: CopilotSession;
    try { copilotSession = await getSession(); }
    catch (err) { return send(res, 503, { error: `CLI connect failed: ${err}` }); }

    // Switch model if requested
    const requestedModel = body.model as string | undefined;
    if (requestedModel) {
      try {
        await copilotSession.rpc.model.switchTo({ modelId: requestedModel });
      } catch (err) {
        // Model switch failed — continue with current model
      }
    }

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      "Connection": "keep-alive",
    });

    // Send current model info
    try {
      const modelInfo = await copilotSession.rpc.model.getCurrent();
      if (modelInfo?.modelId) {
        res.write(`data: ${JSON.stringify({ type: "model", model: modelInfo.modelId })}\n\n`);
      }
    } catch {}

    const unsub = copilotSession.on((event: any) => {
      if (event.type === "assistant.message_delta") {
        res.write(`data: ${JSON.stringify({ type: "content", delta: event.data?.deltaContent })}\n\n`);
      } else if (event.type === "session.idle") {
        res.write(`data: ${JSON.stringify({ type: "done" })}\n\n`);
        unsub();
        res.end();
      } else if (event.type === "session.error") {
        res.write(`data: ${JSON.stringify({ type: "error", message: String(event.data) })}\n\n`);
        unsub();
        res.end();
      }
    });

    copilotSession.send({ prompt: message }).catch((err: unknown) => {
      res.write(`data: ${JSON.stringify({ type: "error", message: String(err) })}\n\n`);
      unsub();
      res.end();
    });
    return;
  }

  send(res, 404, { error: "Not found" });
});

server.listen(PORT, () => {
  console.log(`Copilot server listening on http://localhost:${PORT}`);
});

process.on("SIGTERM", async () => { await resetSession(); process.exit(0); });
