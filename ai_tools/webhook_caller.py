"""
Webhook Caller agent.

initialize: returns service card.
compute: params can be dict or list:
    url (required)
    method: GET/POST/PUT/PATCH/DELETE (default GET)
    headers: optional dict
    body: optional string or object (will be JSON encoded if not string)
Returns status, headers (limited), and body text.
"""

import json
import sys
import urllib.error
import urllib.request
from typing import Any, Dict, Tuple

SERVICE_CARD = {
    "name": "WebhookCaller-v1",
    "description": "Calls arbitrary HTTP endpoints and returns status/body.",
    "inputs": ["url", "method", "headers", "body"],
    "cost_per_op": 0.2,
    "version": "1.0.0",
}


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "url": raw[0] if len(raw) > 0 else None,
            "method": raw[1] if len(raw) > 1 else "GET",
            "headers": raw[2] if len(raw) > 2 else None,
            "body": raw[3] if len(raw) > 3 else None,
        }
    return {}


def prepare_body(body: Any) -> Tuple[bytes, Dict[str, str]]:
    if body is None:
        return b"", {}
    if isinstance(body, str):
        return body.encode("utf-8"), {}
    # assume JSON
    return json.dumps(body).encode("utf-8"), {"Content-Type": "application/json"}


def execute_request(url: str, method: str, headers: Dict[str, str], body: Any) -> Dict[str, Any]:
    data_bytes, extra_headers = prepare_body(body)
    all_headers = headers.copy() if headers else {}
    all_headers.update(extra_headers)

    req = urllib.request.Request(url, data=data_bytes or None, headers=all_headers, method=method.upper())
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            raw_body = resp.read()
            return {
                "status": resp.status,
                "headers": dict(resp.headers),
                "body": raw_body.decode("utf-8", errors="replace"),
            }
    except urllib.error.HTTPError as e:
        raw_body = e.read()
        return {
            "status": e.code,
            "headers": dict(e.headers) if e.headers else {},
            "body": raw_body.decode("utf-8", errors="replace"),
        }
    except urllib.error.URLError as e:
        raise RuntimeError(f"Connection error: {e.reason}") from e


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
        url = params.get("url")
        if not url:
            raise ValueError("url is required")
        http_method = params.get("method", "GET")
        headers = params.get("headers") or {}
        body = params.get("body")

        result = execute_request(str(url), str(http_method), headers, body)
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
