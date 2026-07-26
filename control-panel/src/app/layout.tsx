import type { Metadata } from "next";
import { AppRouterCacheProvider } from "@mui/material-nextjs/v14-appRouter";
import { AppTheme } from "./theme";
import { Providers } from "./providers";

export const metadata: Metadata = {
  title: "Minions Control Panel",
  description: "Manage and monitor your AI minions",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>
        <Providers>
          <AppRouterCacheProvider>
            <AppTheme>{children}</AppTheme>
          </AppRouterCacheProvider>
        </Providers>
      </body>
    </html>
  );
}
