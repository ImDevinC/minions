# GitHub App Setup

This guide walks you through creating and configuring a GitHub App for Minions.

## Why a GitHub App?

Minions uses a GitHub App (not a Personal Access Token) because:

- **Installation tokens**: scoped to specific repos, auto-expire after 1 hour
- **Granular permissions**: read/write only what's needed
- **Audit trail**: all actions attributed to the app, not a user
- **Organization support**: works across orgs without sharing user tokens

## Create the App

1. Go to **GitHub Settings > Developer settings > GitHub Apps**
2. Click **New GitHub App**
3. Fill in the basic info:

| Field | Value |
|-------|-------|
| **App name** | `your-org-minions` (must be globally unique) |
| **Homepage URL** | Your control panel URL or company site |
| **Webhook** | Uncheck "Active" (we don't use webhooks) |

## Required Permissions

Under **Repository permissions**, set:

| Permission | Access | Why |
|------------|--------|-----|
| **Contents** | Read and write | Clone repo, push branches |
| **Pull requests** | Read and write | Create PRs, read existing |
| **Metadata** | Read-only | Required for API access (auto-granted) |

All other permissions can remain **No access**.

Under **Account permissions**: no additional permissions needed.

## Installation Scope

Under **Where can this GitHub App be installed?**:

- Choose **Only on this account** if you only need it for your own org/user
- Choose **Any account** if other orgs will install it

## Generate Private Key

After creating the app:

1. Scroll to **Private keys**
2. Click **Generate a private key**
3. Save the downloaded `.pem` file securely

The private key is used by the orchestrator to authenticate as the app and generate installation tokens.

```bash
# Store the key content in an env var or k8s secret
export GITHUB_APP_PRIVATE_KEY="$(cat your-app.2024-01-01.private-key.pem)"

# Or copy to k8s secret
kubectl create secret generic minions-github-app \
  --namespace=minions \
  --from-literal=GITHUB_APP_ID=123456 \
  --from-file=GITHUB_APP_PRIVATE_KEY=your-app.private-key.pem
```

> **Security**: Never commit the private key. Store it in a secrets manager (Vault, AWS Secrets Manager, etc.) in production.

## Note Your App ID

At the top of the app settings page, you'll see:

```
App ID: 123456
```

Save this, you'll need it as `GITHUB_APP_ID` in the orchestrator config.

## Install the App

1. Go to your app's page (Settings > Developer settings > GitHub Apps > your-app)
2. Click **Install App** in the left sidebar
3. Choose the account/org to install on
4. Select **All repositories** or **Only select repositories**

Each installation gets a unique installation ID. The orchestrator discovers this automatically when looking up tokens by repository owner.

## Verify Setup

Test that your app can generate tokens:

```bash
# In the orchestrator directory
export GITHUB_APP_ID=123456
export GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"

# The orchestrator logs token generation on startup
go run ./cmd/orchestrator
```

You should see log messages like:

```
INF created installation transport installation_id=12345678
INF got installation token repo=your-org/your-repo installation_id=12345678
```

If you see `no GitHub App installation for owner`, the app isn't installed on that account.

## Troubleshooting

### "Resource not accessible by integration"

The app doesn't have the required permissions. Edit the app settings and add the missing permission, then re-approve the updated permissions in each installation.

### "Bad credentials" / 401

- Check that `GITHUB_APP_ID` matches your app
- Verify the private key isn't corrupted (starts with `-----BEGIN RSA PRIVATE KEY-----`)
- Ensure the key wasn't rotated (if so, download the new one)

### "Integration not found" / 404

The app isn't installed on the target account/org. Go to the app settings > Install App and install it.

### Rate Limits

GitHub App installation tokens have generous rate limits (5,000 requests/hour per installation). If you hit limits, you're probably creating too many tokens instead of reusing them. The orchestrator caches tokens automatically.

## Configuration Reference

| Variable | Description |
|----------|-------------|
| `GITHUB_APP_ID` | Numeric app ID from settings page |
| `GITHUB_APP_PRIVATE_KEY` | PEM-encoded private key content |

Both are required for the orchestrator. The Discord bot and control panel don't need these directly, they call the orchestrator which handles GitHub auth.
