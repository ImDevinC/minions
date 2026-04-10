# GitHub Webhook Setup

This guide walks you through configuring GitHub webhooks for Minions. The GitHub webhook service enables PR feedback: when someone @mentions the bot in a PR comment or review, a new minion spawns to address the feedback and pushes to the same branch.

## Overview

```
PR Comment: "@minions-bot please fix the typo on line 42"
                    │
                    ▼
         GitHub Webhook Event
                    │
                    ▼
         github-webhook service
                    │
                    ▼
         Orchestrator spawns minion
                    │
                    ▼
         Minion pushes fix to PR branch
```

## Prerequisites

- A GitHub App already created (see [GitHub App Setup](github-app.md))
- The GitHub App installed on target repositories
- A publicly accessible URL for the webhook endpoint (or use a tunnel for development)

## Configure Webhook in GitHub App

1. Go to [GitHub Developer Settings](https://github.com/settings/apps)
2. Select your Minions GitHub App
3. Go to **Webhooks** section

### Enable Webhook

1. Check **Active** to enable webhooks
2. Set **Webhook URL** to your github-webhook service endpoint:
   ```
   https://your-domain.com/webhook/github
   ```
3. Set **Content type** to `application/json`

### Generate Webhook Secret

Generate a secure random secret:

```bash
openssl rand -hex 32
```

Enter this value in the **Webhook secret** field. Save it, you'll need it as `GITHUB_WEBHOOK_SECRET`.

### Select Events

Under **Subscribe to events**, enable only:

| Event | Why |
|-------|-----|
| **Issue comments** | Detect @mentions in PR comments |
| **Pull request review comments** | Detect @mentions in code review comments |
| **Pull request reviews** | Detect @mentions in review summaries |

Disable all other events to reduce noise.

Click **Save changes**.

## Approved Repos File

The webhook service only processes events from approved repositories. Create a file listing allowed repos:

```bash
# /config/approved-repos.txt
# One repo per line, owner/repo format
# Lines starting with # are comments

myorg/backend-api
myorg/frontend-app
myorg/shared-libs
```

The matching is case-insensitive and exact (no wildcards).

### Mount as ConfigMap (Kubernetes)

```bash
kubectl create configmap github-webhook-repos \
  --namespace=minions \
  --from-file=approved-repos.txt=/path/to/approved-repos.txt
```

## Store Credentials

The webhook service shares GitHub App credentials with the orchestrator:

```bash
# Add webhook secret to existing GitHub App secret
kubectl create secret generic minions-github-app \
  --namespace=minions \
  --from-literal=GITHUB_APP_ID='123456' \
  --from-file=GITHUB_APP_PRIVATE_KEY=./private-key.pem \
  --from-literal=GITHUB_WEBHOOK_SECRET='your-webhook-secret'
```

## Verify Setup

### Local Development

For local testing, use a tunnel service:

```bash
# Start a tunnel (e.g., ngrok)
ngrok http 8080

# Note the HTTPS URL, e.g., https://abc123.ngrok.io
# Update your GitHub App webhook URL to: https://abc123.ngrok.io/webhook/github
```

Start the service:

```bash
cd github-webhook

export GITHUB_APP_ID="123456"
export GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"
export GITHUB_WEBHOOK_SECRET="your-webhook-secret"
export ORCHESTRATOR_URL="http://localhost:8080"
export INTERNAL_API_TOKEN="your-internal-token"
export APPROVED_REPOS_PATH="./approved-repos.txt"

go run ./cmd/github-webhook
```

You should see:

```
INF bot username resolved username=your-app[bot]
INF starting github-webhook server port=8080
```

### Test the Webhook

1. Go to a PR in an approved repository
2. Add a comment mentioning the bot:
   ```
   @your-app please add error handling to this function
   ```
3. Check the webhook service logs for:
   ```
   INF received webhook event type=issue_comment action=created
   INF spawning minion for PR feedback repo=myorg/myrepo pr=123
   ```

### GitHub Webhook Deliveries

To debug webhook issues:

1. Go to your GitHub App settings
2. Click **Advanced** in the sidebar
3. View **Recent Deliveries**
4. Check response codes and payloads

## How It Works

1. User @mentions the bot in a PR comment/review
2. GitHub sends webhook event to github-webhook service
3. Service validates:
   - Webhook signature (using `GITHUB_WEBHOOK_SECRET`)
   - Repository is in approved list
   - Comment mentions the bot username
4. Service extracts:
   - Repository owner/name
   - PR number and branch
   - Comment body as task description
   - Commenter's GitHub username
5. Service calls orchestrator to spawn minion with:
   - Same branch as PR (minion pushes directly, updating the PR)
   - Task = comment body (minus the @mention)
   - Origin = "github" (for tracking)
6. Minion executes task and pushes to PR branch
7. Orchestrator notifies original commenter (via PR comment)

## Troubleshooting

### Webhook Not Received

1. Check GitHub App webhook is **Active**
2. Verify webhook URL is correct and publicly accessible
3. Check **Recent Deliveries** in GitHub App settings for errors

### 401 Signature Verification Failed

The webhook secret doesn't match:
- Verify `GITHUB_WEBHOOK_SECRET` matches the value in GitHub App settings
- Secrets are case-sensitive

### 404 Repo Not Approved

The repository isn't in the approved repos file:
- Add `owner/repo` to the approved repos file
- Restart the service (or remount ConfigMap)

### Bot Username Not Detected

The service fetches the bot username from the GitHub App on startup:
- Check GitHub App credentials are correct
- Verify the GitHub App is properly configured

### Minion Not Spawning

Check orchestrator connectivity:
- Verify `ORCHESTRATOR_URL` is correct
- Check `INTERNAL_API_TOKEN` matches orchestrator's token
- Check orchestrator logs for errors

### PR Branch Conflicts

If the minion's push fails due to conflicts:
- The minion will report the conflict in its PR comment
- Manual resolution may be needed

## Configuration Reference

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY` | Yes | GitHub App private key (PEM content) |
| `GITHUB_WEBHOOK_SECRET` | Yes | Webhook secret for signature verification |
| `ORCHESTRATOR_URL` | Yes | URL of orchestrator service |
| `INTERNAL_API_TOKEN` | Yes | Shared secret for service auth |
| `APPROVED_REPOS_PATH` | No | Path to approved repos file (default: /config/approved-repos.txt) |
| `PORT` | No | HTTP server port (default: 8080) |

## Security Notes

- **Webhook Secret**: Always use a strong random secret. Without it, anyone can forge webhook events.
- **Approved Repos**: Always configure an allowlist. Without it, any repo with the app installed could trigger minions.
- **Private Key**: Never commit the private key. Use secrets management.
- **Network**: The webhook endpoint must be publicly accessible from GitHub's servers.

## Kubernetes Deployment

See the [Kubernetes guide](kubernetes.md) for full deployment instructions. Key resources:

```yaml
# ConfigMap for approved repos
apiVersion: v1
kind: ConfigMap
metadata:
  name: github-webhook-repos
  namespace: minions
data:
  approved-repos.txt: |
    myorg/repo1
    myorg/repo2

---
# Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: github-webhook
  namespace: minions
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: github-webhook
  template:
    spec:
      containers:
        - name: github-webhook
          image: ghcr.io/imdevinc/minions/github-webhook:latest
          ports:
            - containerPort: 8080
          env:
            - name: ORCHESTRATOR_URL
              value: "http://orchestrator.minions.svc.cluster.local:8080"
            - name: APPROVED_REPOS_PATH
              value: "/config/approved-repos.txt"
          envFrom:
            - secretRef:
                name: minions-github-app
            - secretRef:
                name: minions-internal-api
          volumeMounts:
            - name: repos-config
              mountPath: /config
      volumes:
        - name: repos-config
          configMap:
            name: github-webhook-repos
```
