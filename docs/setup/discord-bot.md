# Discord Bot Setup

This guide walks you through creating a Discord bot for Minions.

## Create the Application

1. Go to [Discord Developer Portal](https://discord.com/developers/applications)
2. Click **New Application**
3. Enter a name (e.g., `Minions` or `your-org-minions`)
4. Click **Create**

Note your **Application ID** from the General Information page.

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

```bash
# In the discord-bot directory
export DISCORD_BOT_TOKEN="your-token"
export ORCHESTRATOR_URL="http://localhost:8080"
export INTERNAL_API_TOKEN="your-internal-token"
export OPENROUTER_API_KEY="your-openrouter-key"
export OPENROUTER_CLARIFICATION_MODEL="anthropic/claude-sonnet-4"

go run ./cmd/bot
```

You should see:

```
INF discord bot connected username=Minions discriminator=0000 guilds=1
```

If `guilds=0`, the bot isn't in any servers. Use the invite URL above.

## Troubleshooting

### "Used Disallowed Intents"

You enabled an intent in code but not in the Developer Portal. Go to Bot > Privileged Gateway Intents and toggle Message Content Intent.

### "Unknown Interaction" / Commands Not Working

- Check the bot is online (green dot in Discord)
- Ensure the bot has View Channel permission for the channel
- Verify Message Content Intent is enabled

### "Bot requires code grant"

Don't enable "Require OAuth2 Code Grant" in bot settings. It's for advanced flows the bot doesn't use.

### Bot Can't See Messages

Message Content Intent is probably disabled. Go to Bot > Privileged Gateway Intents and enable it.

## Configuration Reference

| Variable | Description |
|----------|-------------|
| `DISCORD_BOT_TOKEN` | Bot token from Developer Portal |
| `ORCHESTRATOR_URL` | URL of orchestrator service |
| `INTERNAL_API_TOKEN` | Shared secret for service auth |
| `OPENROUTER_API_KEY` | API key for clarification LLM (OpenRouter) |
| `OPENROUTER_CLARIFICATION_MODEL` | OpenRouter model used for clarification checks |
| `DISCORD_ALLOWED_GUILD_ID` | Optional guild ID restriction for command acceptance |
| `DISCORD_ALLOWED_ROLE_ID` | Optional role ID restriction for command acceptance |

## Security Notes

- **Bot Token**: Rotate immediately if exposed. Anyone with the token can impersonate your bot.
- **Message Content Intent**: Only enable if you need it. Minions needs it to read commands.
- **Minimal Permissions**: Don't grant Administrator. Request only what's needed.
