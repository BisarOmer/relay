#!/usr/bin/env python3
"""Drive the relay-engine sidecar over NDJSON for manual smoke checks.

Usage:
  scripts/ipc-probe.py --engine PATH --data-dir DIR [--migrations DIR] < calls.jsonl

Each stdin line is {"method": "...", "params": {...}}. Params may reference
earlier results with "$N.path.to.field" (N = zero-based call index).
Prints one JSON line per call: {"method", "ok", "result" | "error", "ms"}.
"""
import argparse
import json
import subprocess
import sys
import threading
import time
import uuid


def resolve(value, results):
    if isinstance(value, str) and value.startswith("$") and "." in value:
        idx, *path = value[1:].split(".")
        cur = results[int(idx)]
        for p in path:
            cur = cur[int(p)] if isinstance(cur, list) else cur[p]
        return cur
    if isinstance(value, dict):
        return {k: resolve(v, results) for k, v in value.items()}
    if isinstance(value, list):
        return [resolve(v, results) for v in value]
    return value


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--engine", required=True)
    ap.add_argument("--data-dir", required=True)
    ap.add_argument("--migrations")
    ap.add_argument("--events", action="store_true", help="print engine events")
    args = ap.parse_args()

    cmd = [args.engine, "--data-dir", args.data_dir]
    if args.migrations:
        cmd += ["--migrations", args.migrations]
    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            stderr=subprocess.DEVNULL, text=True, bufsize=1)
    pending = {}
    lock = threading.Condition()

    def reader():
        for line in proc.stdout:
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue
            if msg.get("type") == "response":
                with lock:
                    pending[msg["id"]] = msg
                    lock.notify_all()
            elif args.events:
                print(json.dumps({"event": msg.get("event"), "payload": msg.get("payload")}), flush=True)

    threading.Thread(target=reader, daemon=True).start()

    results = []
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw or raw.startswith("#"):
            continue
        call = json.loads(raw)
        if "sleep" in call:
            time.sleep(call["sleep"])
            results.append(None)
            continue
        rid = str(uuid.uuid4())
        params = resolve(call.get("params", {}), results)
        start = time.time()
        proc.stdin.write(json.dumps({"type": "request", "id": rid, "method": call["method"],
                                     "params": params, "timeoutMs": call.get("timeoutMs", 30000)}) + "\n")
        proc.stdin.flush()
        with lock:
            lock.wait_for(lambda: rid in pending, timeout=60)
            resp = pending.pop(rid, None)
        ms = int((time.time() - start) * 1000)
        if resp is None:
            print(json.dumps({"method": call["method"], "ok": False, "error": "no response"}), flush=True)
            results.append(None)
            continue
        out = {"method": call["method"], "ok": resp.get("ok"), "ms": ms}
        if resp.get("ok"):
            out["result"] = resp.get("result")
        else:
            out["error"] = resp.get("error")
        results.append(resp.get("result"))
        print(json.dumps(out), flush=True)

    proc.stdin.close()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()


if __name__ == "__main__":
    main()
