import { AuthOptions } from "next-auth";

// Generic OIDC Provider configuration
// PocketID or any other OIDC-compliant provider can be used
// Access control is handled by the OIDC provider itself

export const authOptions: AuthOptions = {
  providers: [
    {
      id: "oidc",
      name: process.env.OIDC_PROVIDER_NAME || "OIDC",
      type: "oauth",
      wellKnown: process.env.OIDC_ISSUER
        ? `${process.env.OIDC_ISSUER}/.well-known/openid-configuration`
        : undefined,
      issuer: process.env.OIDC_ISSUER,
      clientId: process.env.OIDC_CLIENT_ID!,
      clientSecret: process.env.OIDC_CLIENT_SECRET!,
      authorization: {
        params: {
          scope: process.env.OIDC_SCOPES || "openid email profile",
        },
      },
      idToken: true,
      checks: ["pkce", "state"],
      profile(profile) {
        return {
          id: profile.sub,
          name: profile.name || profile.preferred_username || profile.email,
          email: profile.email,
          image: profile.picture,
        };
      },
    },
  ],
  callbacks: {
    async signIn() {
      // Access control is handled by the OIDC provider (PocketID)
      // If the provider issues a token, the user is authorized
      return true;
    },
    async session({ session, token }) {
      // Add user id to session for use in API calls
      if (session.user && token.sub) {
        session.user.id = token.sub;
      }
      return session;
    },
    async jwt({ token, account }) {
      // Persist user id and access_token to token on initial sign in
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
