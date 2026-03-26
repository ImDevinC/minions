# PRD: Control Panel Guild/Role Login Restriction

**Date:** 2026-03-25

---

## Problem Statement

### What problem are we solving?

The control panel currently allows any Discord user to log in and access minion data, stats, and controls. This is overly permissive for deployments where access should be restricted to members of a specific Discord server (guild) or users with a particular role.

The `DISCORD_ALLOWED_GUILD_ID` and `DISCORD_ALLOWED_ROLE_ID` environment variables already exist and are used by the discord-bot service to restrict command execution, but the control panel does not respect these restrictions, creating an inconsistent access control model.

### Why now?

Proactive improvement. Limiting access before the control panel is widely deployed prevents unauthorized access from becoming a security incident. Aligning access control between the bot and control panel creates a consistent, predictable security model.

### Who is affected?

- **Primary users:** Control panel administrators who want to restrict access
- **Secondary users:** Discord server members who may or may not be granted access based on guild/role membership

---

## Proposed Solution

### Overview

Add optional guild and role-based login restrictions to the control panel using Discord OAuth2 scopes. When `DISCORD_ALLOWED_GUILD_ID` is configured, only members of that guild can log in. When `DISCORD_ALLOWED_ROLE_ID` is also configured, users must additionally have that role within the guild. If neither variable is set, the current behavior (any Discord user can log in) is preserved.

### User Experience

#### User Flow: Successful Login (Authorized User)

1. User clicks "Continue with Discord" on sign-in page
2. Discord OAuth prompts for consent (including `guilds` scope if restriction enabled)
3. User authorizes the application
4. Control panel verifies guild membership (and role if configured)
5. User is redirected to the dashboard

#### User Flow: Failed Login (Unauthorized User)

1. User clicks "Continue with Discord" on sign-in page
2. Discord OAuth prompts for consent
3. User authorizes the application
4. Control panel checks guild membership and finds user is not a member (or lacks required role)
5. User is redirected to `/auth/error?error=AccessDenied`
6. Error page displays: "Access denied. You may not have permission to sign in."

---

## End State

When this PRD is complete, the following will be true:

- [ ] Control panel respects `DISCORD_ALLOWED_GUILD_ID` to restrict login to guild members
- [ ] Control panel respects `DISCORD_ALLOWED_ROLE_ID` to require a specific role (when guild ID is set)
- [ ] When neither env var is set, any Discord user can log in (backward compatible)
- [ ] Documentation explains the additional OAuth scopes and setup requirements
- [ ] Access control is consistent between discord-bot and control-panel

---

## Success Metrics

### Quantitative

| Metric | Current | Target | Measurement Method |
|--------|---------|--------|-------------------|
| Unauthorized logins | Unrestricted | 0 when configured | Auth logs showing AccessDenied |

### Qualitative

- Administrators can confidently deploy the control panel knowing access is restricted
- Setup documentation is clear enough that operators don't misconfigure scopes

---

## Acceptance Criteria

### Feature: Guild Restriction

- [ ] When `DISCORD_ALLOWED_GUILD_ID` is set, only members of that guild can log in
- [ ] Non-members are redirected to error page with AccessDenied
- [ ] OAuth consent includes `guilds` scope when restriction is enabled
- [ ] OAuth consent includes `guilds.members.read` scope when restriction is enabled

### Feature: Role Restriction

- [ ] When `DISCORD_ALLOWED_ROLE_ID` is set (with guild ID), users must have that role
- [ ] Users in the guild without the role are denied access
- [ ] Role check is skipped if `DISCORD_ALLOWED_ROLE_ID` is not set

### Feature: Backward Compatibility

- [ ] When neither env var is set, any Discord user can log in
- [ ] No additional OAuth scopes are requested when restriction is disabled
- [ ] Existing deployments continue to work without configuration changes
- [ ] When env vars are added to existing deployment, control panel must be restarted for scope changes to take effect
- [ ] Existing sessions created before restriction was enabled will fail on next login (expected; document in setup guide)

### Feature: Documentation

- [ ] `docs/setup/discord-app.md` documents the additional OAuth scopes
- [ ] `docs/setup/discord-app.md` notes that bot must be in the guild for `guilds.members.read` to work
- [ ] Control panel configuration reference includes the new env vars

---

## Technical Context

### Existing Patterns

- Guild/role checking in discord-bot: `discord-bot/internal/handler/message.go:459-564` - `isCommandAllowed()` function implements similar logic for bot commands
- NextAuth configuration: `control-panel/src/lib/auth.ts` - Current Discord OAuth setup with `jwt` and `session` callbacks

### Key Files

- `control-panel/src/lib/auth.ts` - Add `signIn` callback and conditional scopes (evaluated at app startup)
- `control-panel/src/app/auth/error/page.tsx` - Already has `AccessDenied` error message (no changes needed)
- `docs/setup/discord-app.md` - Document scope requirements, bot membership prerequisite, and troubleshooting
- `infra/deployments.yaml` - Add env vars to control-panel deployment
- `infra/secrets.yaml` - Add template for guild/role restrictions in minions-control-panel secret
- `control-panel/.env.example` - Document the two optional env vars
- `README.md` - Update Control Panel env var table

### System Dependencies

- Discord API v10 endpoints:
  - `GET /users/@me/guilds/{guild.id}/member` - Get user's member object including roles
- NextAuth Discord provider with custom authorization params

### Error Handling Strategy

The `signIn` callback uses a **fail-closed** security model: deny access on any error to prevent bypass attacks.

| Scenario | HTTP Status | Action | User Message | Log Level |
|----------|-------------|--------|--------------|-----------|
| User not in guild | 404 | Deny login | AccessDenied | Info |
| User in guild, lacks role | 200 (role missing) | Deny login | AccessDenied | Info |
| Discord API 5xx | 500-599 | Deny login | AccessDenied | Error |
| Network timeout (>5s) | N/A | Deny login | AccessDenied | Error |
| Rate limited | 429 | Retry once, then deny | AccessDenied | Warn |
| Invalid token | 401 | Deny login | AccessDenied | Error |
| Bot not in guild | 403 | Deny login | AccessDenied | Error |

**Security rationale:** Fail-closed on infrastructure errors prevents attackers from triggering errors to bypass access control.

### Rate Limiting

Discord API allows ~50 requests/second globally. Each login makes 1 API call (guild member check), giving headroom for ~50 concurrent logins/second. Rate limiting is unlikely but the implementation should:

- Log `X-RateLimit-Remaining` warnings when low
- Retry once with exponential backoff on 429 responses

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Bot not in guild breaks `guilds.members.read` | Medium | High | The `guilds.members.read` scope only works for guilds where the Discord app is installed (bot membership). **Symptom:** All logins fail with AccessDenied, logs show `403 Forbidden`. **Fix:** Verify bot is in guild, add via OAuth2 URL Generator. Document prerequisite in setup guide. |
| Discord API errors during login | Low | Medium | Fail-closed: deny access and log error. User sees generic AccessDenied. |
| Access token expiry | Medium | Low | Discord tokens expire ~7 days. Sessions typically expire sooner. Users re-auth on next login. |
| Rate limiting on login spikes | Low | Medium | 50 req/s headroom; 1 call/login = 50 concurrent logins/s. Add retry with backoff on 429. |
| User removes app from Discord | Low | Low | Standard OAuth flow handles re-consent gracefully |

---

## Alternatives Considered

### Alternative 1: Use Bot Token for Guild/Role Checks

- **Description:** Use the existing `DISCORD_BOT_TOKEN` to call Discord API instead of OAuth scopes
- **Pros:** No additional OAuth scopes needed, works without app being "installed" in guild
- **Cons:** Requires sharing bot token with control-panel, different auth model than standard OAuth
- **Decision:** Rejected. OAuth scopes are the idiomatic approach for user-initiated flows.

### Alternative 2: Re-verify on Every Request

- **Description:** Check guild/role membership on each session refresh, not just at login
- **Pros:** Catches role removals immediately
- **Cons:** More Discord API calls, potential rate limiting, added latency per request
- **Decision:** Deferred. Login-time verification is sufficient for v1. Can add refresh-time checks if needed.

---

## Non-Goals (v1)

Explicitly out of scope for this PRD:

- **Multiple allowed guilds** - Only single guild restriction supported. Multi-guild can be added later if needed.
- **Multiple allowed roles** - Only single role restriction supported. Can extend to role arrays later.
- **Session invalidation on role removal** - Users keep access until session expires. Real-time revocation is complex and not required for v1.
- **Access token refresh** - Discord refresh tokens not used. Tokens expire ~7 days but typical session lifetime is shorter. Users re-authenticate on next login.
- **Audit logging of denied logins** - Would be nice, but not blocking for initial implementation.

---

## Documentation Requirements

- [ ] Update `docs/setup/discord-app.md` with OAuth scope information
- [ ] Add "Prerequisites for Guild Restriction" section with bot membership check
- [ ] Add troubleshooting section for 403 Forbidden errors
- [ ] Document restart requirement when enabling restrictions on existing deployment
- [ ] Update control panel configuration reference with new env vars
- [ ] Update `infra/secrets.yaml` template with commented examples
- [ ] Update `control-panel/.env.example` with new env vars
- [ ] Update `README.md` Control Panel env var table

---

## Testing Requirements

### Manual Testing Checklist

**Setup:**
- Deploy control panel with `DISCORD_ALLOWED_GUILD_ID` set
- Create test Discord users:
  - User A: Member of allowed guild, has required role
  - User B: Member of allowed guild, no required role
  - User C: Not a member of allowed guild

**Test Cases:**

| Test | Setup | Expected Result |
|------|-------|-----------------|
| Unrestricted access | No env vars set | Any Discord user can log in |
| Guild restriction (member) | `GUILD_ID` set, user A | Login succeeds |
| Guild restriction (non-member) | `GUILD_ID` set, user C | AccessDenied error |
| Role restriction (has role) | `GUILD_ID` + `ROLE_ID` set, user A | Login succeeds |
| Role restriction (no role) | `GUILD_ID` + `ROLE_ID` set, user B | AccessDenied error |
| Role-only (invalid config) | `ROLE_ID` set, no `GUILD_ID` | Role check skipped, any user can log in |
| Discord API error | Mock 500 response | AccessDenied, error logged |
| Scope consent | `GUILD_ID` set | OAuth consent shows `guilds` scope |

### Automated Testing

Out of scope for v1 (requires Discord API mocking). Consider for v2 if access control becomes business-critical.

---

## Open Questions

None. All questions resolved during planning.

---

## Appendix

### Glossary

- **Guild:** Discord's term for a server
- **Role:** Discord permission group within a guild
- **OAuth scope:** Permission requested during Discord authorization flow

### References

- [Discord OAuth2 Scopes](https://discord.com/developers/docs/topics/oauth2#shared-resources-oauth2-scopes)
- [Discord Get Current User Guilds](https://discord.com/developers/docs/resources/user#get-current-user-guilds)
- [Discord Get Current User Guild Member](https://discord.com/developers/docs/resources/user#get-current-user-guild-member)
- [NextAuth Discord Provider](https://next-auth.js.org/providers/discord)
