# Minions Repository Agent Guide

## Overview

Minions is a Discord bot-driven AI agent orchestrator that spawns ephemeral Kubernetes pods to execute coding tasks.

## Repository Structure

```
minions/
├── orchestrator/     # Go service - manages Kubernetes pods and task lifecycle
├── discord-bot/      # Go service - Discord integration
├── control-panel/    # Next.js web UI for monitoring
├── devbox/           # Container image for running tasks
├── infra/            # Kubernetes manifests
├── release-configs/  # Semantic-release configs per service
└── schema/           # Shared schemas/types
```

## Skills

### Git Workflow (Required for Commits)

**IMPORTANT**: Before making any commits or submitting changes, load the git-workflow skill:

```
Load skill: git-workflow
```

This skill defines:
- Conventional commit format (required for semantic-release)
- Branch-based development (no direct pushes to main)
- PR submission workflow

Key rules:
1. **Never push directly to main** - Always create a branch and PR
2. **Use conventional commits** - `feat:`, `fix:`, `chore:`, etc.
3. **Include disclosure** - All PRs must include the AI disclosure statement

## CI/CD

- **Commitlint**: Validates conventional commits on PRs
- **Semantic-release**: Creates version tags on merge to main (per-service)
- **Docker build**: Builds images on version tags (`*-v*`)

Images are published to `ghcr.io/imdevinc/minions/<service>`.

## Development

### Building Services

```bash
# Go services (orchestrator, discord-bot)
cd <service> && go build ./...

# Control panel (Next.js)
cd control-panel && npm install && npm run build

# Devbox image
docker build -t devbox ./devbox
```

### Testing

```bash
# Go services
cd <service> && go test ./...

# Control panel
cd control-panel && npm test
```

## Monorepo Versioning

Each service is versioned independently:
- Tags: `<service>-v<semver>` (e.g., `orchestrator-v1.2.0`)
- Releases only trigger for services with changes
- `release-configs/<service>.js` contains per-service semantic-release config
