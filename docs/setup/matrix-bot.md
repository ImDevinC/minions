# Matrix Bot Setup

This guide walks you through creating a Matrix bot for Minions. The Matrix bot provides the same functionality as the Discord bot, allowing users to spawn minions from Matrix rooms.

## Prerequisites

- A Matrix homeserver (e.g., matrix.org, your own Synapse/Dendrite server)
- A Matrix client (Element, Nheko, etc.) for initial setup
- Access to create accounts on your homeserver

## Create Bot Account

### Option A: Register via Client

1. Open your Matrix client (e.g., Element)
2. Create a new account on your homeserver:
   - Username: `minion` (or your preferred bot name)
   - Password: Generate a strong random password
3. Note the full user ID (e.g., `@minion:matrix.org`)

### Option B: Register via Admin API (Synapse)

If you have admin access to a Synapse server:

```bash
# Generate a registration token
curl -X POST "https://your-homeserver/_synapse/admin/v1/register" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "minion",
    "password": "your-secure-password",
    "admin": false
  }'
```

## Get Access Token

The bot needs an access token to authenticate with the homeserver. There are several ways to obtain one:

### Option A: Login via API

```bash
# Login to get access token
curl -X POST "https://your-homeserver/_matrix/client/v3/login" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "m.login.password",
    "identifier": {
      "type": "m.id.user",
      "user": "minion"
    },
    "password": "your-bot-password"
  }'
```

Response:

```json
{
  "user_id": "@minion:your-homeserver.com",
  "access_token": "syt_xxxxx...",
  "device_id": "ABCDEFGH"
}
```

Save the `access_token` value.

### Option B: Get Token from Element

1. Log into Element as the bot user
2. Go to Settings > Help & About
3. Click "Access Token" (at the bottom, under Advanced)
4. Copy the token

> **Warning**: This token grants full access to the bot account. Store it securely.

## Store Credentials

```bash
# Environment variables
export MATRIX_HOMESERVER_URL="https://matrix.org"
export MATRIX_BOT_USER_ID="@minion:matrix.org"
export MATRIX_BOT_ACCESS_TOKEN="syt_xxxxx..."

# Or k8s secret
kubectl create secret generic minions-matrix-bot \
  --namespace=minions \
  --from-literal=MATRIX_HOMESERVER_URL='https://matrix.org' \
  --from-literal=MATRIX_BOT_USER_ID='@minion:matrix.org' \
  --from-literal=MATRIX_BOT_ACCESS_TOKEN='syt_xxxxx...'
```

## Invite Bot to Rooms

The bot needs to be invited to rooms where you want to use it:

1. Open your Matrix client
2. Go to the room where you want to use the bot
3. Invite the bot user (e.g., `@minion:matrix.org`)
4. The bot will auto-accept invites when running

If you've configured `MATRIX_ALLOWED_ROOMS`, the bot will only accept invites to those rooms.

## Room and User Restrictions

You can restrict which rooms and users can interact with the bot:

### Restrict to Specific Rooms

```bash
# Comma-separated list of room IDs
export MATRIX_ALLOWED_ROOMS="!abc123:matrix.org,!def456:matrix.org"
```

To get a room ID:
1. In Element: Room Settings > Advanced > Internal room ID
2. Or check room state via the Matrix API

### Restrict to Specific Users

```bash
# Comma-separated list of user IDs
export MATRIX_ALLOWED_USERS="@alice:matrix.org,@bob:matrix.org"
```

When both are set:
- Bot only responds in allowed rooms
- Bot only responds to allowed users

## Verify Setup

```bash
# In the matrix-bot directory
export MATRIX_HOMESERVER_URL="https://matrix.org"
export MATRIX_BOT_USER_ID="@minion:matrix.org"
export MATRIX_BOT_ACCESS_TOKEN="syt_xxxxx..."
export ORCHESTRATOR_URL="http://localhost:8080"
export INTERNAL_API_TOKEN="your-internal-token"
export OPENROUTER_API_KEY="your-openrouter-key"
export OPENROUTER_CLARIFICATION_MODEL="anthropic/claude-sonnet-4"

go run ./cmd/bot
```

You should see:

```
INF starting matrix-bot version=dev
INF connected to Matrix homeserver user_id=@minion:matrix.org homeserver=https://matrix.org
INF matrix client initial sync completed
```

### Test the Bot

In a Matrix room where the bot is a member:

```
@minion:matrix.org --repo owner/repo Add a /health endpoint
```

The bot should:
1. React or reply to acknowledge the command
2. Start the clarification flow or spawn a minion

## Usage

The command format is identical to the Discord bot:

```
@minion --repo owner/repo <task description>
```

Optional flags:
- `--model provider/model-name` - Override the default model

Examples:

```
@minion:matrix.org --repo myorg/myrepo Fix the login bug

@minion:matrix.org --repo myorg/myrepo --model anthropic/claude-sonnet-4 Add unit tests for the auth module
```

## Troubleshooting

### Bot Not Responding

1. Check the bot is running and connected:
   ```
   INF connected to Matrix homeserver
   ```

2. Verify the bot is in the room:
   - Check room members list
   - Try re-inviting the bot

3. Check room/user restrictions:
   - If `MATRIX_ALLOWED_ROOMS` is set, is this room in the list?
   - If `MATRIX_ALLOWED_USERS` is set, is your user in the list?

### "Failed to create Matrix client"

- Verify `MATRIX_HOMESERVER_URL` is correct and reachable
- Check the homeserver supports the Matrix client-server API

### "Sync error, retrying"

- Network connectivity issue to homeserver
- Access token may have expired or been revoked
- Generate a new access token

### Bot Joins Room But Doesn't Respond

- Message Content may not be reaching the bot (encryption issues)
- Check if the room is encrypted (bot may not support E2EE by default)
- Try in an unencrypted room first

### Rate Limiting

If you see 429 errors, the homeserver is rate limiting requests. The bot will automatically retry with backoff.

## Configuration Reference

| Variable | Required | Description |
|----------|----------|-------------|
| `MATRIX_HOMESERVER_URL` | Yes | Matrix homeserver URL (e.g., https://matrix.org) |
| `MATRIX_BOT_USER_ID` | Yes | Bot user ID (e.g., @minion:matrix.org) |
| `MATRIX_BOT_ACCESS_TOKEN` | Yes | Bot access token |
| `ORCHESTRATOR_URL` | Yes | URL of orchestrator service |
| `INTERNAL_API_TOKEN` | Yes | Shared secret for service auth |
| `OPENROUTER_API_KEY` | Yes | API key for clarification LLM (OpenRouter) |
| `OPENROUTER_CLARIFICATION_MODEL` | Yes | OpenRouter model used for clarification checks |
| `MATRIX_ALLOWED_ROOMS` | No | Comma-separated room IDs to restrict to |
| `MATRIX_ALLOWED_USERS` | No | Comma-separated user IDs to allow |
| `CONTROL_PANEL_URL` | No | URL for minion status page links in messages |
| `PORT` | No | HTTP server port for webhooks (default: 8081) |

## Security Notes

- **Access Token**: Rotate immediately if exposed. Anyone with the token can impersonate your bot.
- **Room Restrictions**: Use `MATRIX_ALLOWED_ROOMS` in production to prevent unauthorized use.
- **User Restrictions**: Use `MATRIX_ALLOWED_USERS` to limit who can spawn minions.
- **Unencrypted Rooms**: The bot works best in unencrypted rooms. E2EE support requires additional setup.
