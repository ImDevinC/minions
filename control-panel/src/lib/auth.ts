import { AuthOptions, Account } from "next-auth";
import DiscordProvider from "next-auth/providers/discord";

// Build OAuth scopes at module load time (restart required for changes)
// When guild restriction is enabled, we need guilds + guilds.members.read to verify membership
const DISCORD_ALLOWED_GUILD_ID = process.env.DISCORD_ALLOWED_GUILD_ID;
const DISCORD_ALLOWED_ROLE_ID = process.env.DISCORD_ALLOWED_ROLE_ID;

// Warn about invalid configuration at module load time
if (DISCORD_ALLOWED_ROLE_ID && !DISCORD_ALLOWED_GUILD_ID) {
  console.warn(
    "[auth] DISCORD_ALLOWED_ROLE_ID is set but DISCORD_ALLOWED_GUILD_ID is not. " +
      "Role restriction will be ignored and any Discord user can log in."
  );
}

const DISCORD_API_BASE = "https://discord.com/api/v10";
const DISCORD_API_TIMEOUT_MS = 5000;

interface DiscordGuildMember {
  roles: string[];
  // Other fields exist but we only need roles
}

interface GuildMemberCheckResult {
  success: boolean;
  member?: DiscordGuildMember;
  errorReason?: "not_member" | "api_error" | "rate_limited" | "timeout";
}

/**
 * Fetch guild member info from Discord API with retry logic for rate limits.
 * Returns member data on success, or error info on failure.
 */
async function fetchGuildMember(
  accessToken: string,
  guildId: string
): Promise<GuildMemberCheckResult> {
  const url = `${DISCORD_API_BASE}/users/@me/guilds/${guildId}/member`;

  const attemptFetch = async (
    isRetry: boolean
  ): Promise<GuildMemberCheckResult> => {
    const controller = new AbortController();
    const timeoutId = setTimeout(
      () => controller.abort(),
      DISCORD_API_TIMEOUT_MS
    );

    try {
      const response = await fetch(url, {
        headers: {
          Authorization: `Bearer ${accessToken}`,
        },
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      // Check rate limit headers and warn if getting close
      const rateLimitRemaining = response.headers.get("X-RateLimit-Remaining");
      if (rateLimitRemaining !== null) {
        const remaining = parseInt(rateLimitRemaining, 10);
        if (!isNaN(remaining) && remaining < 10) {
          console.warn(
            `[auth] Discord API rate limit warning: ${remaining} requests remaining`
          );
        }
      }

      if (response.ok) {
        const member: DiscordGuildMember = await response.json();
        return { success: true, member };
      }

      if (response.status === 404) {
        // User is not a member of the guild
        return { success: false, errorReason: "not_member" };
      }

      if (response.status === 429 && !isRetry) {
        // Rate limited, retry once after 1 second
        console.warn("[auth] Discord API rate limited, retrying in 1 second");
        await new Promise((resolve) => setTimeout(resolve, 1000));
        return attemptFetch(true);
      }

      if (response.status === 429) {
        // Rate limited on retry, give up
        console.error("[auth] Discord API rate limited after retry");
        return { success: false, errorReason: "rate_limited" };
      }

      // 5xx or other error: fail-closed
      console.error(
        `[auth] Discord API error: ${response.status} ${response.statusText}`
      );
      return { success: false, errorReason: "api_error" };
    } catch (error) {
      clearTimeout(timeoutId);

      if (error instanceof Error && error.name === "AbortError") {
        console.error("[auth] Discord API request timed out");
        return { success: false, errorReason: "timeout" };
      }

      console.error("[auth] Discord API fetch failed:", error);
      return { success: false, errorReason: "api_error" };
    }
  };

  return attemptFetch(false);
}

/**
 * Verify that a user has the required role in the guild.
 * Returns true if role check passes or is not configured.
 */
function verifyRoleMembership(member: DiscordGuildMember): boolean {
  if (!DISCORD_ALLOWED_ROLE_ID) {
    // No role restriction configured, allow all guild members
    return true;
  }

  // Handle null/undefined roles gracefully (treat as empty array)
  const roles = member.roles ?? [];

  if (roles.length === 0) {
    console.info(
      `[auth] User denied access: no roles in guild, required role ${DISCORD_ALLOWED_ROLE_ID}`
    );
    return false;
  }

  if (!roles.includes(DISCORD_ALLOWED_ROLE_ID)) {
    console.info(
      `[auth] User denied access: does not have required role ${DISCORD_ALLOWED_ROLE_ID}`
    );
    return false;
  }

  return true;
}

/**
 * Verify that a user is a member of the allowed guild.
 * Returns true if the user is authorized, false otherwise.
 */
async function verifyGuildMembership(account: Account): Promise<boolean> {
  if (!DISCORD_ALLOWED_GUILD_ID) {
    // No guild restriction configured, allow all users
    return true;
  }

  if (!account.access_token) {
    console.error("[auth] No access token available for guild verification");
    return false;
  }

  const result = await fetchGuildMember(
    account.access_token,
    DISCORD_ALLOWED_GUILD_ID
  );

  if (result.success) {
    // Guild membership verified, now check role if configured
    return verifyRoleMembership(result.member!);
  }

  // Log appropriately based on error type
  if (result.errorReason === "not_member") {
    console.info(
      `[auth] User denied access: not a member of guild ${DISCORD_ALLOWED_GUILD_ID}`
    );
  }
  // API errors already logged in fetchGuildMember

  return false;
}

function buildDiscordScopes(): string {
  const baseScopes = ["identify", "email"];

  if (DISCORD_ALLOWED_GUILD_ID) {
    // guilds: list user's guilds, guilds.members.read: get member info in specific guild
    baseScopes.push("guilds", "guilds.members.read");
  }

  return baseScopes.join(" ");
}

const discordScopes = buildDiscordScopes();

export const authOptions: AuthOptions = {
  providers: [
    DiscordProvider({
      clientId: process.env.DISCORD_CLIENT_ID!,
      clientSecret: process.env.DISCORD_CLIENT_SECRET!,
      authorization: {
        params: {
          scope: discordScopes,
        },
      },
    }),
  ],
  callbacks: {
    async signIn({ account }) {
      // Verify guild membership when restriction is enabled.
      // Returns false to redirect to /auth/error?error=AccessDenied.
      // Note: Users with existing sessions won't hit this until they re-login,
      // at which point they'll see the new OAuth consent with guild scopes.
      if (!account) {
        console.error("[auth] No account in signIn callback");
        return false;
      }
      return verifyGuildMembership(account);
    },
    async session({ session, token }) {
      // Add discord_id to session for use in API calls
      if (session.user && token.sub) {
        session.user.id = token.sub;
      }
      return session;
    },
    async jwt({ token, account }) {
      // Persist discord id and access_token to token on initial sign in
      // access_token is needed for Discord API calls (guild/role verification)
      if (account) {
        token.sub = account.providerAccountId;
        token.accessToken = account.access_token;
      }
      return token;
    },
  },
  pages: {
    signIn: "/auth/signin",
    error: "/auth/error",
  },
  session: {
    strategy: "jwt",
  },
};
