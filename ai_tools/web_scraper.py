"""
Web Scraper agent (lightweight, requests/html.parser).

initialize: returns service card.
compute: params dict or list:
    url: target URL (required)
    selector: optional substring to look for in text
    format: "text" (default), "json", "raw", "screenshot"

For "screenshot" this sample returns a not-supported error to avoid heavy deps.
"""

import html.parser
import json
import sys
import urllib.error
import urllib.request
from typing import Any, Dict

SERVICE_CARD = {
    "name": "WebScraper-v1",
    "description": "Fetches a URL and returns text/json content; screenshot is placeholder.",
    "inputs": ["url", "selector", "format"],
    "cost_per_op": 0.3,
    "version": "1.0.0",
}


class TextExtractor(html.parser.HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.chunks = []

    def handle_data(self, data: str) -> None:
        text = data.strip()
        if text:
            self.chunks.append(text)

    def text(self) -> str:
        return " ".join(self.chunks)


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "url": raw[0] if len(raw) > 0 else None,
            "selector": raw[1] if len(raw) > 1 else None,
            "format": raw[2] if len(raw) > 2 else "text",
        }
    return {}


def fetch_url(url: str) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": "prxs-scraper"})
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            return resp.read()
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace") if e.fp else ""
        raise RuntimeError(f"HTTP {e.code}: {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Network error: {e.reason}") from e


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
        fmt = (params.get("format") or "text").lower()
        selector = params.get("selector")

        raw = fetch_url(url)

        if fmt == "json":
            resp["result"] = json.loads(raw.decode("utf-8"))
        elif fmt == "raw":
            resp["result"] = raw.decode("utf-8", errors="replace")
        elif fmt == "screenshot":
            raise RuntimeError("Screenshot not supported in lightweight sample agent; use headless browser provider.")
        else:  # text
            parser = TextExtractor()
            parser.feed(raw.decode("utf-8", errors="replace"))
            text = parser.text()
            if selector:
                idx = text.lower().find(str(selector).lower())
                if idx >= 0:
                    excerpt = text[max(0, idx - 80) : idx + 80]
                    resp["result"] = {"excerpt": excerpt, "full_text": text[:5000]}
                else:
                    resp["result"] = {"excerpt": "", "full_text": text[:5000]}
            else:
                resp["result"] = {"full_text": text[:5000]}
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
