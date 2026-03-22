# Discord App Setup

This guide walks you through creating the Discord application for Minions. You'll create one application that serves both purposes:

- **Bot**: listens for `@minion` commands in your server
- **OAuth2**: lets users sign in to the control panel

## Create the Application

1. Go to [Discord Developer Portal](https://discord.com/developers/applications)
2. Click **New Application**
3. Enter a name (e.g., `Minions` or `your-org-minions`)
4. Click **Create**

Note your **Application ID** (also called Client ID) from the General Information page.

## Bot Setup

### Create the Bot

1. Go to **Bot** in the left sidebar
2. Click **Add Bot** (if not already created)
3. Under **Token**, click **Reset Token** and copy it

Store this as `DISCORD_BOT_TOKEN` in your secrets:

```bash
# Environment variable
export DISCORD_BOT_TOKEN="your-bot-token"

# Or k8s secret
kubectl create secret generic minions-discord-bot \
  --namespace=minions \
  --from-literal=DISCORD_BOT_TOKEN=your-bot-token
```

> **Security**: Bot tokens grant full access to your bot. Never commit them, rotate if leaked.

### Configure Intents

Under **Privileged Gateway Intents**, enable:

| Intent | Required | Why |
|--------|----------|-----|
| **Message Content Intent** | Yes | Read command text from `@minion` mentions |
| **Server Members Intent** | No | Not used |
| **Presence Intent** | No | Not used |

**Message Content Intent** became privileged in April 2022. Without it, the bot cannot read message content and commands will fail silently.

### Bot Permissions

When adding the bot to a server, it needs these permissions:

| Permission | Why |
|------------|-----|
| **Send Messages** | Reply to commands, post status updates |
| **Add Reactions** | React with 🤔 to acknowledge commands |
| **Read Message History** | Access messages for reply handling |
| **View Channels** | See channels where it's mentioned |

The bot does **not** need:
- Administrator
- Manage Messages
- Manage Channels
- Any moderation permissions

Minimal permissions = smaller blast radius if the token leaks.

## OAuth2 Setup (Control Panel)

The control panel uses Discord OAuth2 to authenticate users. This uses the same application, different credentials.

### Get Client Credentials

1. Go to **OAuth2** in the left sidebar
2. Note your **Client ID** (same as Application ID)
3. Click **Reset Secret** under **Client Secret** and copy it

Store these for the control panel:

```bash
export DISCORD_CLIENT_ID="your-application-id"
export DISCORD_CLIENT_SECRET="your-client-secret"
```

### Configure Redirects

Under **OAuth2 > Redirects**, add your control panel callback URLs:

```
# Local development
http://localhost:3000/api/auth/callback/discord

# Production
https://your-control-panel.example.com/api/auth/callback/discord
```

Add one redirect per environment. NextAuth requires exact URL matching.

### Scopes

NextAuth's Discord provider requests these scopes by default:

| Scope | Why |
|-------|-----|
| `identify` | Get user ID and username |
| `email` | Get user's email (optional, for display) |

No additional scopes needed. The control panel doesn't post as the user or access their servers.

## Add Bot to Your Server

Generate an invite URL:

1. Go to **OAuth2 > URL Generator**
2. Select scopes: `bot`
3. Select bot permissions:
   - Send Messages
   - Add Reactions
   - Read Message History
   - View Channels
4. Copy the generated URL
5. Open it and select your server

The bot will appear offline until you start the discord-bot service.

## Verify Setup

### Test the Bot

```bash
# In the discord-bot directory
export DISCORD_BOT_TOKEN="your-token"
export ORCHESTRATOR_URL="http://localhost:8080"
export INTERNAL_API_TOKEN="your-internal-token"
export ANTHROPIC_API_KEY="your-anthropic-key"

go run ./cmd/bot
```

You should see:

```
INF discord bot connected username=Minions discriminator=0000 guilds=1
```

If `guilds=0`, the bot isn't in any servers. Use the invite URL above.

### Test OAuth2

```bash
# In the control-panel directory
export DISCORD_CLIENT_ID="your-app-id"
export DISCORD_CLIENT_SECRET="your-secret"
export NEXTAUTH_SECRET="random-32-char-string"
export NEXTAUTH_URL="http://localhost:3000"

npm run dev
```

Visit `http://localhost:3000/api/auth/signin` and click "Sign in with Discord". You should be redirected to Discord, then back to your app.

## Troubleshooting

### "Used Disallowed Intents"

You enabled an intent in code but not in the Developer Portal. Go to Bot > Privileged Gateway Intents and toggle Message Content Intent.

### "Unknown Interaction" / Commands Not Working

- Check the bot is online (green dot in Discord)
- Ensure the bot has View Channel permission for the channel
- Verify Message Content Intent is enabled

### OAuth2 "Invalid redirect_uri"

The redirect URL doesn't match exactly. Common issues:
- Trailing slash mismatch (`/callback` vs `/callback/`)
- HTTP vs HTTPS mismatch
- Wrong port number

Add the exact URL shown in the error to OAuth2 > Redirects.

### "Bot requires code grant"

Don't enable "Require OAuth2 Code Grant" in bot settings. It's for advanced flows the bot doesn't use.

### Bot Can't See Messages

Message Content Intent is probably disabled. Go to Bot > Privileged Gateway Intents and enable it.

## Configuration Reference

### Discord Bot Service

| Variable | Description |
|----------|-------------|
| `DISCORD_BOT_TOKEN` | Bot token from Developer Portal |
| `ORCHESTRATOR_URL` | URL of orchestrator service |
| `INTERNAL_API_TOKEN` | Shared secret for service auth |
| `ANTHROPIC_API_KEY` | API key for clarification LLM |

### Control Panel

| Variable | Description |
|----------|-------------|
| `DISCORD_CLIENT_ID` | Application ID / Client ID |
| `DISCORD_CLIENT_SECRET` | OAuth2 client secret |
| `NEXTAUTH_SECRET` | Random string for session encryption |
| `NEXTAUTH_URL` | Public URL of the control panel |

## Security Notes

- **Bot Token**: Rotate immediately if exposed. Anyone with the token can impersonate your bot.
- **Client Secret**: Less critical than bot token, but still rotate if leaked.
- **Message Content Intent**: Only enable if you need it. Minions needs it to read commands.
- **Minimal Permissions**: Don't grant Administrator. Request only what's needed.
