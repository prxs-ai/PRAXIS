# PRXS Registry Docker

This directory contains Docker configuration for running the PRXS Registry (Lighthouse) node.

## Files

- `Dockerfile` - Multi-stage build configuration for the registry binary
- `docker-compose.yaml` - Docker Compose configuration for running the registry

## Quick Start

### Build and Run

From this directory:

```bash
docker-compose up -d
```

This will:
1. Build the registry binary using a multi-stage build
2. Create a minimal Alpine-based runtime image
3. Start the registry container with persistent storage
4. Expose P2P ports (4001) and REST API port (5001)

### View Logs

```bash
docker-compose logs -f registry
```

### Stop the Registry

```bash
docker-compose down
```

### Stop and Remove Volumes

```bash
docker-compose down -v
```

## Configuration

### Environment Variables

You can customize the registry by editing the `environment` section in `docker-compose.yaml`:

- `PORT` - P2P port (default: 4001)
- `KEY_FILE` - Path to persistent key file (default: /app/keys/registry.key)
- `DEV_MODE` - Enable LAN/Dev mode (default: true)
- `MIN_STAKE` - Minimum stake required to register (default: 10.0)

### Ports

- **4001/tcp** - P2P TCP port
- **4001/udp** - P2P UDP port (QUIC)
- **5001/tcp** - REST API port (P2P port + 1000)

### Volumes

- `registry-keys` - Persistent storage for the registry's private key
- `registry-data` - Additional data storage

## REST API

Once running, the REST API is available at `http://localhost:5001/api/v1`

### Endpoints

- `GET /api/v1/services` - Get all registered services
- `GET /api/v1/services/search?q=<query>` - Search for services
- `GET /api/v1/services/:name` - Get specific service by name

Example:
```bash
curl http://localhost:5001/api/v1/services
```

## Health Check

The container includes a health check that verifies the REST API is responding:

```bash
docker-compose ps
```

The STATUS column will show `healthy` when the service is ready.

## Getting the Registry Address

To get the registry's multiaddr for connecting providers and clients:

```bash
docker-compose logs registry | grep "Node ID"
```

Look for output like:
```
Node ID: QmXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
 - /ip4/172.18.0.2/tcp/4001/p2p/QmXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
 - /ip4/172.18.0.2/udp/4001/quic-v1/p2p/QmXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
```

## Connecting from Host

Providers and clients running on the host machine can connect using:
```
/ip4/127.0.0.1/tcp/4001/p2p/<PEER_ID>
```

Or:
```
/ip4/127.0.0.1/udp/4001/quic-v1/p2p/<PEER_ID>
```

## Resource Limits

The default configuration includes resource limits:
- CPU: 2 cores max, 0.5 cores reserved
- Memory: 1GB max, 256MB reserved

Adjust these in the `deploy.resources` section of `docker-compose.yaml` as needed.

## Production Deployment

For production:

1. Set `DEV_MODE=false` in the environment variables
2. Configure proper network settings (remove bridge network, use host network or proper port forwarding)
3. Set up proper monitoring and alerting
4. Consider using external volumes for backups
5. Review and adjust resource limits based on load

## Troubleshooting

### Container won't start
```bash
docker-compose logs registry
```

### Check health status
```bash
docker inspect prxs-registry | grep -A 10 Health
```

### Access container shell
```bash
docker-compose exec registry /bin/sh
```

### Rebuild after code changes
```bash
docker-compose build --no-cache
docker-compose up -d
```
