"use client";

import Link from "next/link";
import AppBar from "@mui/material/AppBar";
import Toolbar from "@mui/material/Toolbar";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Box from "@mui/material/Box";
import { useSession } from "next-auth/react";
import { SignOutButton } from "./sign-out-button";

interface AppHeaderProps {
  title?: string;
  backHref?: string;
  backLabel?: string;
}

export function AppHeader({ title = "Minions Control Panel", backHref, backLabel }: AppHeaderProps) {
  const { data: session } = useSession();

  return (
    <AppBar position="static" elevation={0} sx={{ mb: 4, backgroundColor: "#1e293b" }}>
      <Toolbar>
        <Typography variant="h6" component="div" sx={{ flexGrow: 1 }}>
          {title}
        </Typography>
        <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
          {backHref && (
            <Button component={Link} href={backHref} color="inherit">
              {backLabel ?? "Back"}
            </Button>
          )}
          <Button component={Link} href="/stats" color="inherit">
            Stats
          </Button>
          <Typography variant="body2" color="text.secondary">
            {session?.user?.name}
          </Typography>
          <SignOutButton />
        </Box>
      </Toolbar>
    </AppBar>
  );
}
