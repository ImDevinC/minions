# Minions

Discord bot that spawns ephemeral Kubernetes pods to execute coding tasks using AI (OpenCode).

```
@minion --repo owner/repo Add a /health endpoint
```

## Architecture

```
Discord       Orchestrator (Go)          K8s Pods           Control Panel
   │               │                        │                     │
   │──@minion────▶│──spawn pod────────────▶│                     │
   │               │◀──SSE events──────────│                     │
   │               │──WebSocket broadcast──────────────────────▶│
   │◀──PR URL─────│◀──callback─────────────│                     │
```

The orchestrator manages pod lifecycle, streams events to a PostgreSQL database, and broadcasts live updates to the control panel via WebSocket.

See [docs/architecture.md](docs/architecture.md) for the full design.

## Components

| Directory | Description |
|-----------|-------------|
| `orchestrator/` | Go service: REST API, K8s pod management, event streaming |
| `discord-bot/` | Go service: Discord gateway, command parsing, clarification flow |
| `control-panel/` | Next.js app: dashboard, live logs, cost tracking |
| `devbox/` | Dockerfile + entrypoint for minion pods |
| `schema/` | PostgreSQL migrations |
| `infra/` | Kubernetes manifests |

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+
- PostgreSQL 13+ (or Docker)
- Kubernetes cluster (minikube, kind, or cloud)
- GitHub App
- Discord Application

### 1. Database

```bash
# Start postgres (or use existing)
docker run -d --name minions-db \
  -e POSTGRES_DB=minions \
  -e POSTGRES_USER=minions \
  -e POSTGRES_PASSWORD=minions \
  -p 5432:5432 \
  postgres:16-alpine

# Run migrations
for f in schema/migrations/*.sql; do
  psql "postgres://minions:minions@localhost:5432/minions" -f "$f"
done
```

### 2. Orchestrator

```bash
cd orchestrator

# Set environment variables
export DATABASE_URL="postgres://minions:minions@localhost:5432/minions"
export INTERNAL_API_TOKEN="your-secret-token"
export GITHUB_APP_ID="123456"
export GITHUB_APP_PRIVATE_KEY="$(cat path/to/private-key.pem)"

go run ./cmd/orchestrator
```

### 3. Discord Bot

```bash
cd discord-bot

export DISCORD_BOT_TOKEN="your-discord-bot-token"
export ORCHESTRATOR_URL="http://localhost:8080"
export INTERNAL_API_TOKEN="your-secret-token"
export OPENROUTER_API_KEY="sk-or-..."
# Optional restrictions (single guild and role)
# export DISCORD_ALLOWED_GUILD_ID="123456789012345678"
# export DISCORD_ALLOWED_ROLE_ID="987654321098765432"

go run ./cmd/bot
```

### 4. Control Panel

```bash
cd control-panel

cp .env.example .env.local
# Edit .env.local with your Discord OAuth credentials

npm install
npm run dev
```

### 5. Devbox Image

```bash
docker build -t minions-devbox:latest devbox/
```

## Setup Guides

- [GitHub App Setup](docs/setup/github-app.md) - Create and configure the GitHub App
- [Discord App Setup](docs/setup/discord-app.md) - Create the Discord bot and OAuth
- [Kubernetes Deployment](docs/setup/kubernetes.md) - Deploy to a cluster

## Environment Variables

### Orchestrator

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `INTERNAL_API_TOKEN` | Yes | Shared secret for service auth |
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY` | Yes | GitHub App private key (PEM) |
| `DISCORD_BOT_WEBHOOK_URL` | No | Webhook for Discord notifications |

### Discord Bot

| Variable | Required | Description |
|----------|----------|-------------|
| `DISCORD_BOT_TOKEN` | Yes | Discord bot token |
| `ORCHESTRATOR_URL` | Yes | Orchestrator API URL |
| `INTERNAL_API_TOKEN` | Yes | Shared secret for API auth |
| `OPENROUTER_API_KEY` | Yes | For clarification LLM calls (OpenRouter) |
| `DISCORD_ALLOWED_GUILD_ID` | No | Restrict commands to a specific guild ID |
| `DISCORD_ALLOWED_ROLE_ID` | No | Restrict commands to users with a specific role ID |

### Control Panel

| Variable | Required | Description |
|----------|----------|-------------|
| `NEXTAUTH_URL` | Yes | App URL (e.g., http://localhost:3000) |
| `NEXTAUTH_SECRET` | Yes | Random 32+ char secret |
| `DISCORD_CLIENT_ID` | Yes | Discord OAuth client ID |
| `DISCORD_CLIENT_SECRET` | Yes | Discord OAuth client secret |
| `ORCHESTRATOR_URL` | Yes | Orchestrator API URL |
| `INTERNAL_API_TOKEN` | Yes | Shared secret for API auth |
| `DISCORD_ALLOWED_GUILD_ID` | No | Restrict login to members of this guild ID |
| `DISCORD_ALLOWED_ROLE_ID` | No | Restrict login to users with this role ID (requires guild ID) |

> **Note:** Adding or changing `DISCORD_ALLOWED_GUILD_ID` or `DISCORD_ALLOWED_ROLE_ID` requires restarting the control panel because OAuth scopes are configured at startup.

## License

MIT
