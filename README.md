# PRAXIS

A decentralized service mesh runtime built on libp2p, enabling Python agents to discover, register, and execute services across a peer-to-peer network.

## Overview

PRAXIS provides the core infrastructure for building and deploying decentralized services:

- **Decentralized Discovery** - Service registry with libp2p-based peer discovery
- **Agent Runtime** - Execute Python agents as network services
- **Persistence** - Optional Redis backend for state management
- **Semantic Search** - Qdrant-powered service discovery

## Components

### Registry (Lighthouse)

The central service discovery node:
- Discovers and tracks service providers
- Enforces staking requirements
- Exposes REST API for service queries

**Flags:**
- `-port` - libp2p port (default: 4001)
- `-api-port` - REST API port (default: 8080)
- `-redis` - Redis address for persistence (optional)
- `-qdrant-enabled` - Enable semantic search
- `-min-stake` - Minimum stake to register (default: 10.0)

### Node

Runs in two modes:

**Provider Mode:**
- Executes Python agents as services
- Registers services in the registry
- Handles service calls over libp2p

**Client Mode:**
- Discovers services via registry
- Calls providers directly over libp2p

## Sample Agents

The `ai_tools/` directory contains example services:

- **calc.py** - Mathematical operations (sqrt, factorial)
- **defi_apy.py** - DeFi pool APY lookup via DefiLlama
- **tavily_search.py** - Web search integration
- **pdf_summarizer.py** - Document summarization
- **monitor_agent.py** - Network monitoring (ping, HTTP, TLS)
- **web_scraper.py** - Web scraping
- **webhook_caller.py** - Webhook execution
- **binance.py** - Cryptocurrency data
- **home_assistant.py** - Home automation integration

## REST API

Registry exposes REST API at `http://localhost:8080/api/v1`:

- `GET /health` - Health check
- `GET /services` - List all services
- `GET /services_full` - Services with full metadata
- `GET /services/search?q=<query>` - Text search
- `GET /services/:name` - Get specific service
- `GET /services/semantic_search?q=<query>&k=5` - Semantic search (Qdrant)
- `GET /registry/info` - Get registry Peer ID and bootstrap multiaddrs

## Prerequisites

Install the Python SDK for agents:

```bash
pip install git+ssh://git@github.com/prxs-ai/praxis-original-services.git
```

## Building

```bash
# Build Go binaries
go build -o bin/registry ./cmd/registry
go build -o bin/node ./cmd/node
```

## Running

### 1. Start Registry

```bash
./bin/registry -port 4001 -api-port 8080
```

The registry will output its Peer ID and multiaddrs:
```
REGISTRY ONLINE.
Node ID: QmYKnfLLihnLTdJzJB9xhuPDXw2LidR24QaaRkVXhZuTSt
 - /ip4/127.0.0.1/udp/4001/quic-v1/p2p/QmYKnfLLihnLTdJzJB9xhuPDXw2LidR24QaaRkVXhZuTSt
```

### 2. Start Provider Node

```bash
./bin/node -mode provider -agent ai_tools/calc.py -port 4002 \
  -bootstrap /ip4/127.0.0.1/udp/4001/quic-v1/p2p/<REGISTRY_PEER_ID>
```

On first run, visit `http://127.0.0.1:8090/stake` to complete mock staking.

### 3. Call Service via Client

```bash
./bin/node -mode client -query math -args '[25, "sqrt"]' \
  -bootstrap /ip4/127.0.0.1/udp/4001/quic-v1/p2p/<REGISTRY_PEER_ID>
```

Example output:
```
--- RESULT ---
5
--------------
```

## Semantic Search (Qdrant)

Enable semantic service discovery:

1. Start Qdrant: `docker run -p 6333:6333 qdrant/qdrant`
2. Start registry with `-qdrant-enabled=true`
3. Query: `GET /api/v1/services/semantic_search?q=math&k=5`

## Redis Persistence

Enable state persistence:

1. Start Redis: `docker run -d -p 6379:6379 redis:7-alpine`
2. Start registry with `-redis localhost:6379`
3. Registry state survives restarts

## Docker Deployment

Production-ready Docker setup:

```bash
cd docker/registry
docker-compose up -d
```

See `docker/registry/README.md` for details.

## Project Structure

```
.
├── cmd/
│   ├── registry/    # Registry binary
│   └── node/        # Node binary (provider/client)
├── common/          # Shared Go code
├── storage/         # Redis storage implementation
├── agent/           # Go agent runtime
├── ai_tools/        # Example Python agents
└── docker/          # Docker configurations
```

## Related Repositories

- **[praxis-ui](https://github.com/prxs-ai/praxis-ui)** - React-based web interface and chat assistant
- **[praxis-original-services](https://github.com/prxs-ai/praxis-original-services)** - Python SDK (`prxs_sdk`)
