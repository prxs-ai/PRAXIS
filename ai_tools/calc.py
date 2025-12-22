import sys
import json
import math

from prxs_sdk.utils import call_llm_embeddings


SERVICE_CARD = {
    "name": "MathOracle-v1",
    "description": "Calculates square roots and factorial. Python powered.",
    "inputs": ["number", "operation"],
    "cost_per_op": 0.5,
    "version": "1.0.0",
    "tags": ["math", "sqrt", "factorial", "calculator"],
}


def _attach_embedding_via_llm() -> None:
    """
    Optionally compute an embedding for this service using LLM Embeddings API.
    This is best-effort: if API key or library are not available,
    the agent still works, just without a model-based embedding.
    """
    if call_llm_embeddings is None:
        return

    try:
        text = "{} {} {}".format(
            SERVICE_CARD["name"],
            SERVICE_CARD.get("description", ""),
            " ".join(SERVICE_CARD.get("tags", [])),
        ).strip()
        vec = call_llm_embeddings(text)
        # Store as-is; registry currently uses its own 64-dim hash embedding for Qdrant.
        SERVICE_CARD["embedding"] = vec
    except Exception:
        # Swallow any embedding error to avoid breaking the agent.
        return


_attach_embedding_via_llm()

def process_request(req):
    method = req.get("method")
    params = req.get("params", [])
    req_id = req.get("id")

    response = {"id": req_id, "result": None, "error": None}

    if method == "initialize":
        response["result"] = SERVICE_CARD
        return response

    if method == "compute":
        try:
            val = float(params[0])
            op = params[1]
            if op == "sqrt":
                response["result"] = math.sqrt(val)
            elif op == "factorial":
                response["result"] = math.factorial(int(val))
            else:
                response["error"] = "Unknown operation"
        except Exception as e:
            response["error"] = str(e)
        return response

    response["error"] = "Method not found"
    return response

def main():
    # Process stdin line by line (NDJSON style)
    for line in sys.stdin:
        if not line.strip():
            continue
        try:
            req = json.loads(line)
            resp = process_request(req)
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()
        except json.JSONDecodeError:
            pass

if __name__ == "__main__":
    main()
