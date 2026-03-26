import { AuthOptions } from "next-auth";
import DiscordProvider from "next-auth/providers/discord";

// Build OAuth scopes at module load time (restart required for changes)
// When guild restriction is enabled, we need guilds + guilds.members.read to verify membership
const DISCORD_ALLOWED_GUILD_ID = process.env.DISCORD_ALLOWED_GUILD_ID;

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
