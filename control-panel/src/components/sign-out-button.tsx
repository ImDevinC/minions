"use client";

import { signOut } from "next-auth/react";
import Button from "@mui/material/Button";
import LogoutIcon from "@mui/icons-material/Logout";

export function SignOutButton() {
  return (
    <Button
      variant="outlined"
      size="small"
      startIcon={<LogoutIcon />}
      onClick={() => signOut()}
    >
      Sign Out
    </Button>
  );
}
