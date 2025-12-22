import sys
import json
import urllib.request
import urllib.error

SERVICE_CARD = {
    "name": "PriceOracle-v1",
    "description": "Returns the symbol price from Binance. Python powered.",
    "inputs": ["symbol"],
    "cost_per_op": 0.5,
    "version": "1.0.0"
}

def process_request(req):
    method = req.get("method")
    params = req.get("params", [])
    req_id = req.get("id")

    response = {"id": req_id, "result": None, "error": None}

    if method == "initialize":
        response["result"] = SERVICE_CARD
        return response

    if method == "compute":
        symbol = params[0]
        url = f'https://api.binance.com/api/v3/ticker/price?symbol={symbol}'
        try:
            with urllib.request.urlopen(url) as resp:
                if resp.status == 200:
                    data = json.loads(resp.read().decode('utf-8'))
                    response["result"] = data["price"]
                else:
                    response["error"] = f"HTTP error: {response.status}"
        except urllib.error.URLError as e:
            response["error"] = f"HTTP connection error: {e}"

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
