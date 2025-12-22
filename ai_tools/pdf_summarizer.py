"""
PDF/Document summarizer agent.

initialize: returns service card.
compute: params dict or list:
    source: URL or base64-encoded content (required)
    task: "summary" (default) or "qa"
    question: optional when task=qa
    provider: optional LLM provider ("openai"/"huggingface") for better summaries

Attempts to extract text; if pypdf is available it will parse PDFs, otherwise
falls back to naive extraction. Summaries use LLM if keys are present, else a
simple heuristic (truncate).
"""

import base64
import io
import json
import sys
import urllib.error
import urllib.request
from typing import Any, Dict

SERVICE_CARD = {
    "name": "PDFSummarizer-v1",
    "description": "Fetches a document/PDF and returns summary or QA answer.",
    "inputs": ["source", "task", "question", "provider"],
    "cost_per_op": 0.7,
    "version": "1.0.0",
}

try:
    import pypdf  # type: ignore

    HAS_PYPDF = True
except Exception:  # pylint: disable=broad-except
    HAS_PYPDF = False


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "source": raw[0] if len(raw) > 0 else None,
            "task": raw[1] if len(raw) > 1 else "summary",
            "question": raw[2] if len(raw) > 2 else None,
            "provider": raw[3] if len(raw) > 3 else None,
        }
    return {}


def fetch_source(source: str) -> bytes:
    if source.startswith("http://") or source.startswith("https://"):
        try:
            with urllib.request.urlopen(source, timeout=20) as resp:
                return resp.read()
        except urllib.error.HTTPError as e:
            raise RuntimeError(f"HTTP {e.code}") from e
        except urllib.error.URLError as e:
            raise RuntimeError(f"Network error: {e.reason}") from e
    # assume base64
    return base64.b64decode(source)


def extract_text(data: bytes) -> str:
    if HAS_PYPDF:
        try:
            reader = pypdf.PdfReader(io.BytesIO(data))  # type: ignore
            text = "".join(page.extract_text() or "" for page in reader.pages)
            return text
        except Exception:  # pylint: disable=broad-except
            pass
    try:
        decoded = data.decode("utf-8")
        return decoded
    except Exception:  # pylint: disable=broad-except
        return ""


def summarize_with_llm(text: str, params: Dict[str, Any]) -> str:
    # Try OpenAI/HF via simple HTTP; reuse minimal inline client
    provider = (params.get("provider") or "openai").lower()
    prompt = f"Summarize the following text in 5 bullet points:\n\n{text[:6000]}"
    if provider == "huggingface":
        from sample_agents.openai_hf_inference import call_hf  # type: ignore

        return call_hf(params.get("model") or "gpt2", prompt, 200).get("text", "")
    from sample_agents.openai_hf_inference import call_openai  # type: ignore

    return call_openai(params.get("model") or "gpt-4o-mini", prompt, 200).get("text", "")


def qa_with_llm(text: str, question: str, params: Dict[str, Any]) -> str:
    provider = (params.get("provider") or "openai").lower()
    prompt = f"Answer based only on the context:\n\nContext:\n{text[:6000]}\n\nQuestion: {question}"
    if provider == "huggingface":
        from sample_agents.openai_hf_inference import call_hf  # type: ignore

        return call_hf(params.get("model") or "gpt2", prompt, 200).get("text", "")
    from sample_agents.openai_hf_inference import call_openai  # type: ignore

    return call_openai(params.get("model") or "gpt-4o-mini", prompt, 200).get("text", "")


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
        source = params.get("source")
        if not source:
            raise ValueError("source is required")
        data = fetch_source(source)
        text = extract_text(data)
        if not text:
            raise RuntimeError("Failed to extract text")

        task = (params.get("task") or "summary").lower()
        if task == "qa":
            question = params.get("question")
            if not question:
                raise ValueError("question is required for qa")
            try:
                answer = qa_with_llm(text, question, params)
            except Exception:  # pylint: disable=broad-except
                answer = text[:500]  # fallback
            resp["result"] = {"answer": answer}
        else:
            try:
                summary = summarize_with_llm(text, params)
            except Exception:  # pylint: disable=broad-except
                summary = text[:500]
            resp["result"] = {"summary": summary}
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
