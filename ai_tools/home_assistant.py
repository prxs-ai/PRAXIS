"""
Home Assistant / IoT agent.

initialize: returns service card.
compute: params dict or list:
    base_url: HA base URL (e.g. http://192.168.1.10:8123)
    token: long-lived access token (optional if env HOME_ASSISTANT_TOKEN)
    entity_id: target entity (light.kitchen)
    action: "get_state" (default), "set_state", "call_service"
    value: optional state value/payload
    domain: optional for call_service (default inferred from entity_id prefix)
    service: optional for call_service (default "turn_on")
"""

import json
import os
import sys
import urllib.error
import urllib.request
from typing import Any, Dict

SERVICE_CARD = {
    "name": "HomeAssistant-v1",
    "description": "Reads or sets entity state via Home Assistant REST API.",
    "inputs": ["base_url", "entity_id", "action", "value", "service"],
    "cost_per_op": 0.4,
    "version": "1.0.0",
}


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "base_url": raw[0] if len(raw) > 0 else None,
            "entity_id": raw[1] if len(raw) > 1 else None,
            "action": raw[2] if len(raw) > 2 else "get_state",
            "value": raw[3] if len(raw) > 3 else None,
        }
    return {}


def auth_headers(token_override: str = None) -> Dict[str, str]:
    token = token_override or os.getenv("HOME_ASSISTANT_TOKEN")
    if not token:
        raise RuntimeError("HOME_ASSISTANT_TOKEN is not set")
    return {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}


def http_request(url: str, method: str, headers: Dict[str, str], payload: Any = None) -> Dict[str, Any]:
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            body = resp.read().decode("utf-8")
            return {"status": resp.status, "body": json.loads(body) if body else {}}
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8") if e.fp else ""
        raise RuntimeError(f"HTTP {e.code}: {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Network error: {e.reason}") from e


def get_state(base_url: str, entity_id: str, headers: Dict[str, str]) -> Dict[str, Any]:
    url = f"{base_url.rstrip('/')}/api/states/{entity_id}"
    return http_request(url, "GET", headers)


def set_state(base_url: str, entity_id: str, value: Any, headers: Dict[str, str]) -> Dict[str, Any]:
    url = f"{base_url.rstrip('/')}/api/states/{entity_id}"
    payload = {"state": value}
    return http_request(url, "POST", headers, payload)


def call_service(base_url: str, entity_id: str, domain: str, service: str, value: Any, headers: Dict[str, str]) -> Dict[str, Any]:
    url = f"{base_url.rstrip('/')}/api/services/{domain}/{service}"
    payload = {"entity_id": entity_id}
    if value is not None:
        payload["value"] = value
    return http_request(url, "POST", headers, payload)


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
        base_url = params.get("base_url")
        entity_id = params.get("entity_id")
        if not base_url or not entity_id:
            raise ValueError("base_url and entity_id are required")

        headers = auth_headers(params.get("token"))
        action = (params.get("action") or "get_state").lower()
        value = params.get("value")

        if action == "get_state":
            resp["result"] = get_state(base_url, entity_id, headers)
        elif action == "set_state":
            resp["result"] = set_state(base_url, entity_id, value, headers)
        elif action == "call_service":
            domain = params.get("domain") or entity_id.split(".")[0]
            service = params.get("service") or "turn_on"
            resp["result"] = call_service(base_url, entity_id, domain, service, value, headers)
        else:
            raise ValueError("Unknown action. Use get_state, set_state, or call_service.")
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
