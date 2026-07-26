"use client";

import { signIn } from "next-auth/react";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Box from "@mui/material/Box";
import VpnKeyIcon from "@mui/icons-material/VpnKey";

export default function SignInPage() {
  return (
    <Container
      maxWidth="sm"
      sx={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <Paper elevation={4} sx={{ p: 4, width: "100%", textAlign: "center" }}>
        <Typography variant="h4" component="h1" gutterBottom>
          Minions Control Panel
        </Typography>
        <Typography variant="body1" color="text.secondary" sx={{ mb: 4 }}>
          Sign in to manage your minions
        </Typography>
        <Button
          variant="contained"
          size="large"
          fullWidth
          startIcon={<VpnKeyIcon />}
          onClick={() => signIn("oidc", { callbackUrl: "/" })}
        >
          Sign In
        </Button>
      </Paper>
    </Container>
  );
}
