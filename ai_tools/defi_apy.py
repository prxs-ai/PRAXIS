"""
DeFi APY Lookup Agent.

Protocol:
- initialize: returns SERVICE_CARD
- compute: expects params with token_symbol (required), chain (optional),
  min_tvl (optional), limit (optional)

The agent queries DefiLlama API to find the best DeFi pools by APY.

Environment:
- No API key required (DefiLlama is public)
"""

import json
import sys
import urllib.error
import urllib.request
from typing import Any, Dict, List

SERVICE_CARD = {
    "name": "DefiAPY-v1",
    "description": "Finds the best DeFi pools by APY for a given token across multiple chains using DefiLlama.",
    "inputs": ["token_symbol", "chain", "min_tvl", "limit"],
    "cost_per_op": 0.3,
    "version": "1.0.0",
    "tags": ["defi", "apy", "yield", "farming", "pools", "defillama"],
}


def build_params(raw: Any) -> Dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, list):
        return {
            "token_symbol": raw[0] if len(raw) > 0 else None,
            "chain": raw[1] if len(raw) > 1 else None,
            "min_tvl": raw[2] if len(raw) > 2 else None,
            "limit": raw[3] if len(raw) > 3 else 5,
        }
    return {}


def fetch_pools() -> List[Dict[str, Any]]:
    url = "https://yields.llama.fi/pools"
    req = urllib.request.Request(url, headers={"User-Agent": "prxs-defi-apy-agent"})

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read().decode("utf-8")
            data = json.loads(body)
            return data.get("data", [])
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", errors="replace") if e.fp else ""
        raise RuntimeError(f"HTTP {e.code}: {body}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"Network error: {e.reason}") from e


def filter_and_sort_pools(
    pools: List[Dict[str, Any]],
    token_symbol: str,
    chain: str = None,
    min_tvl: float = None,
    limit: int = 5,
) -> List[Dict[str, Any]]:
    """
    Filter pools by token and chain, then sort by APY.

    Args:
        pools: List of pool data from DefiLlama
        token_symbol: Token symbol to search for (case-insensitive)
        chain: Optional chain filter (e.g., "Ethereum", "Arbitrum")
        min_tvl: Optional minimum TVL in USD
        limit: Maximum number of results to return

    Returns:
        Filtered and sorted list of pools
    """
    token_lower = token_symbol.lower()
    chain_lower = chain.lower() if chain else None

    filtered = []
    for pool in pools:
        if not pool.get("symbol") or pool.get("apy") is None:
            continue

        symbol = pool.get("symbol", "").lower()
        if token_lower not in symbol:
            continue

        if chain_lower and pool.get("chain", "").lower() != chain_lower:
            continue

        tvl = pool.get("tvlUsd", 0)
        if min_tvl and tvl < min_tvl:
            continue

        filtered.append(pool)

    sorted_pools = sorted(filtered, key=lambda p: p.get("apy", 0), reverse=True)

    return sorted_pools[:limit]


def format_pool_result(pool: Dict[str, Any]) -> Dict[str, Any]:
    return {
        "pool_id": pool.get("pool"),
        "project": pool.get("project"),
        "symbol": pool.get("symbol"),
        "chain": pool.get("chain"),
        "apy": round(pool.get("apy", 0), 2),
        "tvl_usd": round(pool.get("tvlUsd", 0), 2),
        "apr_base": round(pool.get("apyBase", 0), 2) if pool.get("apyBase") else None,
        "apr_reward": round(pool.get("apyReward", 0), 2) if pool.get("apyReward") else None,
        "il_risk": pool.get("ilRisk", "unknown"),
        "exposure": pool.get("exposure", "unknown"),
    }


def search_defi_pools(
    token_symbol: str,
    chain: str = None,
    min_tvl: float = None,
    limit: int = 5,
) -> Dict[str, Any]:
    """
    Search for the best DeFi pools by APY.

    Args:
        token_symbol: Token symbol to search for
        chain: Optional chain filter
        min_tvl: Optional minimum TVL filter
        limit: Number of results to return

    Returns:
        Dictionary with search results and metadata
    """
    if not token_symbol:
        raise ValueError("token_symbol is required")

    all_pools = fetch_pools()

    top_pools = filter_and_sort_pools(all_pools, token_symbol, chain, min_tvl, limit)

    formatted_pools = [format_pool_result(p) for p in top_pools]

    return {
        "token": token_symbol,
        "chain": chain,
        "min_tvl": min_tvl,
        "total_found": len(formatted_pools),
        "pools": formatted_pools,
    }


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
        token_symbol = params.get("token_symbol")
        if not token_symbol:
            raise ValueError("token_symbol is required")

        chain = params.get("chain")
        min_tvl = params.get("min_tvl")
        limit = params.get("limit", 5)

        if min_tvl is not None:
            min_tvl = float(min_tvl)

        limit = int(limit)

        result = search_defi_pools(token_symbol, chain, min_tvl, limit)
        resp["result"] = result
    except Exception as exc:
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
