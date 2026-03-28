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

No additional scopes needed for basic operation. The control panel doesn't post as the user or access their servers.

#### Additional Scopes for Guild Restriction

When `DISCORD_ALLOWED_GUILD_ID` is set, the control panel requests additional scopes to verify guild membership:

| Scope | Why |
|-------|-----|
| `guilds` | List user's guilds (not actually used, but Discord requires it) |
| `guilds.members.read` | Check if user is a member of the allowed guild |

These scopes are automatically added when guild restriction is enabled. See [Guild Restriction](#guild-restriction) for setup details.

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
export OPENROUTER_API_KEY="your-openrouter-key"
export OPENROUTER_CLARIFICATION_MODEL="anthropic/claude-sonnet-4"

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

### Guild Restriction: 403 Forbidden

The control panel failed to verify guild membership. Common causes:

1. **Bot not in guild**: The Discord application's bot must be a member of the guild for `guilds.members.read` to work. Add the bot using the invite URL from [Add Bot to Your Server](#add-bot-to-your-server).

2. **Wrong guild ID**: Double-check `DISCORD_ALLOWED_GUILD_ID` matches your server. Right-click server name > Copy Server ID (requires Developer Mode).

3. **User not in guild**: The user trying to log in isn't a member of the allowed guild. This is expected behavior (they're denied access).

### Guild Restriction: AccessDenied

User was denied access to the control panel. Check the control panel logs for details:

- "User not a member of allowed guild" - User isn't in the Discord server
- "User does not have required role" - User is in server but missing the required role
- "Discord API error" - Temporary Discord API issue; user should retry

## Configuration Reference

### Discord Bot Service

| Variable | Description |
|----------|-------------|
| `DISCORD_BOT_TOKEN` | Bot token from Developer Portal |
| `ORCHESTRATOR_URL` | URL of orchestrator service |
| `INTERNAL_API_TOKEN` | Shared secret for service auth |
| `OPENROUTER_API_KEY` | API key for clarification LLM (OpenRouter) |
| `OPENROUTER_CLARIFICATION_MODEL` | OpenRouter model used for clarification checks |
| `DISCORD_ALLOWED_GUILD_ID` | Optional guild ID restriction for command acceptance |
| `DISCORD_ALLOWED_ROLE_ID` | Optional role ID restriction for command acceptance |

### Control Panel

| Variable | Description |
|----------|-------------|
| `DISCORD_CLIENT_ID` | Application ID / Client ID |
| `DISCORD_CLIENT_SECRET` | OAuth2 client secret |
| `NEXTAUTH_SECRET` | Random string for session encryption |
| `NEXTAUTH_URL` | Public URL of the control panel |
| `DISCORD_ALLOWED_GUILD_ID` | Optional: restrict login to members of this guild |
| `DISCORD_ALLOWED_ROLE_ID` | Optional: restrict login to members with this role (requires `DISCORD_ALLOWED_GUILD_ID`) |

## Guild Restriction

You can restrict control panel access to members of a specific Discord server (guild), and optionally require a specific role.

### Prerequisites

Before enabling guild restriction:

1. **The Discord application's bot must be a member of the guild**. The `guilds.members.read` OAuth scope requires bot membership to verify user roles.
   - Verify: Open your Discord server > Server Settings > Members > search for your bot name
   - If the bot isn't listed, use the bot invite URL from [Add Bot to Your Server](#add-bot-to-your-server)

2. **Get your Guild ID**:
   - Enable Developer Mode in Discord: User Settings > App Settings > Advanced > Developer Mode
   - Right-click your server name > Copy Server ID

3. **Get your Role ID** (optional):
   - Server Settings > Roles > right-click the role > Copy Role ID

### Configuration

Set these environment variables for the control panel:

```bash
# Restrict to guild members
export DISCORD_ALLOWED_GUILD_ID="123456789012345678"

# Optional: also require a specific role
export DISCORD_ALLOWED_ROLE_ID="987654321098765432"
```

**Important notes:**

- **Restart required**: The control panel must be restarted after changing these values. OAuth scopes are determined at startup.
- **Existing users see new consent**: Users who previously logged in will see a new OAuth consent screen requesting the `guilds.members.read` scope.
- **Role without guild is ignored**: Setting `DISCORD_ALLOWED_ROLE_ID` without `DISCORD_ALLOWED_GUILD_ID` has no effect (any Discord user can log in). The control panel logs a warning at startup.

## Security Notes

- **Bot Token**: Rotate immediately if exposed. Anyone with the token can impersonate your bot.
- **Client Secret**: Less critical than bot token, but still rotate if leaked.
- **Message Content Intent**: Only enable if you need it. Minions needs it to read commands.
- **Minimal Permissions**: Don't grant Administrator. Request only what's needed.
