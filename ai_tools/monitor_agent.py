"""
Monitoring agent: ping/http/tls checks.

initialize: returns service card.
compute: params dict or list:
    target: hostname or URL (required)
    kind: "ping" (default), "http", "tls"
    threshold_ms: optional threshold to flag slow responses
"""

import json
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime
from typing import Any, Dict

SERVICE_CARD = {
    "name": "Monitor-v1",
    "description": "Performs ping, HTTP, or TLS expiry checks.",
    "inputs": ["target", "kind", "threshold_ms"],
    "cost_per_op": 0.2,
    "version": "1.0.0",
}


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "target": raw[0] if len(raw) > 0 else None,
            "kind": raw[1] if len(raw) > 1 else "ping",
            "threshold_ms": raw[2] if len(raw) > 2 else None,
        }
    return {}


def do_ping(target: str) -> Dict[str, Any]:
    proc = subprocess.run(["ping", "-c", "3", target], capture_output=True, text=True)
    return {"code": proc.returncode, "stdout": proc.stdout, "stderr": proc.stderr}


def do_http(url: str) -> Dict[str, Any]:
    req = urllib.request.Request(url, headers={"User-Agent": "prxs-monitor"})
    start = time.time()
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            body = resp.read()
            latency_ms = (time.time() - start) * 1000
            return {
                "status": resp.status,
                "latency_ms": latency_ms,
                "body_snippet": body.decode("utf-8", errors="replace")[:2000],
            }
    except urllib.error.HTTPError as e:
        return {"status": e.code, "latency_ms": (time.time() - start) * 1000, "error": e.read().decode("utf-8", errors="replace")}
    except urllib.error.URLError as e:
        raise RuntimeError(f"HTTP error: {e.reason}") from e


def do_tls(host: str, port: int = 443) -> Dict[str, Any]:
    ctx = ssl.create_default_context()
    with socket.create_connection((host, port), timeout=5) as sock:
        with ctx.wrap_socket(sock, server_hostname=host) as ssock:
            cert = ssock.getpeercert()
    expires_str = cert.get("notAfter")
    expires_at = datetime.strptime(expires_str, "%b %d %H:%M:%S %Y %Z")
    days_left = (expires_at - datetime.utcnow()).days
    return {"expires_at": expires_str, "days_left": days_left}


def process_request(req: Dict[str, Any]) -> Dict[str, Any]:
    method = req.get("method")
    params = build_params(req.get("params"))
    resp: Dict[str, Any] = {"id": req.get("id"), "result": None, "error": None}

    if method == "initialize":
        resp["result"] = SERVICE_CARD
        return resp

    if method != "compute":
        resp["error"] = "Method not found"
        return resp

    try:
        target = params.get("target")
        if not target:
            raise ValueError("target is required")
        kind = (params.get("kind") or "ping").lower()
        threshold = params.get("threshold_ms")

        if kind == "ping":
            result = do_ping(target)
        elif kind == "http":
            result = do_http(target)
        elif kind == "tls":
            host, port = (target.split(":") + ["443"])[:2]
            result = do_tls(host, int(port))
        else:
            raise ValueError("kind must be ping/http/tls")

        if threshold and isinstance(result, dict) and "latency_ms" in result:
            result["slow"] = result["latency_ms"] > float(threshold)

        resp["result"] = result
    except Exception as exc:  # pylint: disable=broad-except
        resp["error"] = str(exc)

    return resp


def main() -> None:
    for line in sys.stdin:
        if not line.strip():
            continue
        try:
            req = json.loads(line)
            response = process_request(req)
            sys.stdout.write(json.dumps(response) + "\n")
            sys.stdout.flush()
        except json.JSONDecodeError:
            continue


if __name__ == "__main__":
    main()
