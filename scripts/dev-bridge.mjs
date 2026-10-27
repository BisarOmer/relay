#!/usr/bin/env node
// Development-only bridge that lets the Relay UI run in a normal browser
// against the real Go engine. It mirrors the Tauri host: it spawns the engine,
// speaks NDJSON over stdio, and exposes it on loopback behind a bearer token.
//
//   node scripts/dev-bridge.mjs [--engine path] [--data-dir path] [--port 1422]
//
// Then start Vite with VITE_RELAY_BRIDGE_URL / VITE_RELAY_BRIDGE_TOKEN set (the
// bridge writes them to apps/desktop/.env.development.local). Production builds
// ignore these variables.

import { spawn } from "node:child_process";
import { randomBytes, randomUUID, timingSafeEqual } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import http from "node:http";
import path from "node:path";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, cur, i, all) => (cur.startsWith("--") ? [...acc, [cur.slice(2), all[i + 1]]] : acc), []),
);
const enginePath = path.resolve(args.engine ?? path.join(root, "engine/bin/relay-engine"));
const dataDir = path.resolve(args["data-dir"] ?? path.join(root, ".tools/dev-data"));
const port = Number(args.port ?? 1422);
const token = process.env.RELAY_BRIDGE_TOKEN ?? randomBytes(24).toString("hex");
const allowedOrigins = new Set(["http://localhost:1420", "http://127.0.0.1:1420"]);
const HOST_ONLY = new Set(["shutdown"]);
const MAX_TIMEOUT_MS = 10 * 60_000;

let child = null;
let ready = false;
let hello = null;
let lastError = null;
let restarts = 0;
const pending = new Map();
const clients = new Set();

function broadcast(value) {
  const frame = `data: ${JSON.stringify(value)}\n\n`;
  for (const res of clients) res.write(frame);
}

function failAll(reason) {
  for (const [, p] of pending) p.resolve({ ok: false, error: { code: "engine_unavailable", message: reason, retryable: true } });
  pending.clear();
}

function send(method, params, timeoutMs) {
  return new Promise((resolve) => {
    if (!child) return resolve({ ok: false, error: { code: "engine_unavailable", message: "engine is not running", retryable: true } });
    const id = randomUUID();
    const timer = setTimeout(() => {
      pending.delete(id);
      child?.stdin.write(`${JSON.stringify({ type: "request", id: randomUUID(), method: "cancel", params: { targetId: id } })}\n`);
      resolve({ ok: false, error: { code: "timeout", message: "engine did not respond in time", retryable: true } });
    }, timeoutMs + 1000);
    pending.set(id, {
      resolve: (v) => {
        clearTimeout(timer);
        resolve(v);
      },
    });
    child.stdin.write(`${JSON.stringify({ type: "request", id, method, params: params ?? null, timeoutMs })}\n`);
  });
}

function start() {
  return new Promise((resolve) => {
    mkdirSync(dataDir, { recursive: true });
    ready = false;
    const proc = spawn(enginePath, ["--data-dir", dataDir], { stdio: ["pipe", "pipe", "pipe"] });
    child = proc;
    let resolved = false;
    const done = (ok) => {
      if (!resolved) {
        resolved = true;
        resolve(ok);
      }
    };
    proc.on("error", (err) => {
      lastError = `failed to start engine: ${err.message}`;
      console.error(lastError);
      done(false);
    });
    readline.createInterface({ input: proc.stderr }).on("line", (l) => console.error(`engine: ${l}`));
    readline.createInterface({ input: proc.stdout }).on("line", async (line) => {
      let v;
      try {
        v = JSON.parse(line);
      } catch {
        return;
      }
      if (v.type === "response") {
        const p = pending.get(v.id);
        if (p) {
          pending.delete(v.id);
          p.resolve(v.ok ? { ok: true, result: v.result ?? null } : { ok: false, error: v.error ?? {} });
        }
        return;
      }
      if (v.type === "event") {
        if (v.event === "engine.ready") {
          const h = await send("hello", { protocolVersion: 1, client: "relay-dev-bridge", clientVersion: "dev" }, 10_000);
          hello = h.ok ? h.result : null;
          ready = true;
          lastError = null;
          broadcast({ type: "event", event: "engine.ready", payload: hello });
          done(true);
          return;
        }
        broadcast(v);
      }
    });
    proc.on("exit", (code) => {
      if (child !== proc) return;
      child = null;
      ready = false;
      failAll("engine exited");
      lastError = code === null ? "engine process exited" : `engine process exited (code ${code})`;
      broadcast({ type: "event", event: "engine.exited", payload: { message: lastError, code } });
      done(false);
    });
    setTimeout(() => {
      if (!ready) {
        lastError = "engine did not become ready";
        done(false);
      }
    }, 15_000);
  });
}

async function stop() {
  const proc = child;
  if (!proc) return;
  child = null;
  ready = false;
  proc.stdin.end();
  await new Promise((r) => {
    const t = setTimeout(() => {
      proc.kill("SIGKILL");
      r();
    }, 6000);
    proc.on("exit", () => {
      clearTimeout(t);
      r();
    });
  });
  failAll("engine stopped");
}

function status() {
  return { ready, protocolVersion: hello?.protocolVersion ?? 0, restarts, lastError, hello };
}

function authorized(req, url) {
  const header = req.headers.authorization ?? "";
  const given = header.startsWith("Bearer ") ? header.slice(7) : (url.searchParams.get("token") ?? "");
  const a = Buffer.from(given);
  const b = Buffer.from(token);
  return a.length === b.length && timingSafeEqual(a, b);
}

function readBody(req, limit = 8 * 1024 * 1024) {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks = [];
    req.on("data", (c) => {
      size += c.length;
      if (size > limit) {
        reject(new Error("body too large"));
        req.destroy();
      } else chunks.push(c);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

const server = http.createServer(async (req, res) => {
  const origin = req.headers.origin;
  if (origin && !allowedOrigins.has(origin)) {
    res.writeHead(403).end();
    return;
  }
  if (origin) {
    res.setHeader("access-control-allow-origin", origin);
    res.setHeader("vary", "origin");
    res.setHeader("access-control-allow-headers", "authorization, content-type");
    res.setHeader("access-control-allow-methods", "GET, POST");
  }
  if (req.method === "OPTIONS") {
    res.writeHead(204).end();
    return;
  }
  const url = new URL(req.url ?? "/", `http://127.0.0.1:${port}`);
  if (!authorized(req, url)) {
    res.writeHead(401, { "content-type": "application/json" }).end(JSON.stringify({ ok: false, error: { code: "authorization_error", message: "bad token" } }));
    return;
  }
  const json = (code, body) => res.writeHead(code, { "content-type": "application/json" }).end(JSON.stringify(body));
  try {
    if (req.method === "GET" && url.pathname === "/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache", connection: "keep-alive" });
      res.write(": connected\n\n");
      clients.add(res);
      const ping = setInterval(() => res.write(": ping\n\n"), 15_000);
      req.on("close", () => {
        clearInterval(ping);
        clients.delete(res);
      });
      return;
    }
    if (req.method === "GET" && url.pathname === "/status") return json(200, status());
    if (req.method === "POST" && url.pathname === "/restart") {
      await stop();
      restarts++;
      await start();
      return json(200, status());
    }
    if (req.method === "POST" && url.pathname === "/rpc") {
      const body = JSON.parse(await readBody(req));
      if (typeof body.method !== "string") return json(400, { ok: false, error: { code: "validation_error", message: "method is required" } });
      if (HOST_ONLY.has(body.method)) return json(200, { ok: false, error: { code: "authorization_error", message: "method not available to the UI" } });
      if (!ready) return json(200, { ok: false, error: { code: "engine_unavailable", message: "engine is not ready", retryable: true } });
      const timeout = Math.min(Math.max(Number(body.timeoutMs) || 30_000, 100), MAX_TIMEOUT_MS);
      return json(200, await send(body.method, body.params, timeout));
    }
    json(404, { ok: false, error: { code: "not_found", message: "not found" } });
  } catch (err) {
    json(400, { ok: false, error: { code: "validation_error", message: String(err.message ?? err) } });
  }
});

const envFile = path.join(root, "apps/desktop/.env.development.local");
writeFileSync(envFile, `VITE_RELAY_BRIDGE_URL=http://127.0.0.1:${port}\nVITE_RELAY_BRIDGE_TOKEN=${token}\n`, { mode: 0o600 });

server.listen(port, "127.0.0.1", async () => {
  console.error(`relay dev bridge on http://127.0.0.1:${port} (engine ${enginePath}, data ${dataDir})`);
  console.error(`wrote ${path.relative(root, envFile)}; start Vite after this line`);
  if (!(await start())) console.error(`engine failed to start: ${lastError}`);
});

for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, async () => {
    await stop();
    process.exit(0);
  });
}
