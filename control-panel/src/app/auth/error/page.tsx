"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Alert from "@mui/material/Alert";
import WarningIcon from "@mui/icons-material/Warning";

function ErrorContent() {
  const searchParams = useSearchParams();
  const error = searchParams.get("error");

  const errorMessages: Record<string, string> = {
    Configuration: "There is a problem with the server configuration.",
    AccessDenied: "Access denied. You may not have permission to sign in.",
    Verification: "The verification link has expired or has already been used.",
    Default: "An error occurred during authentication.",
  };

  const message = errorMessages[error || ""] || errorMessages.Default;

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
        <WarningIcon color="error" sx={{ fontSize: 64, mb: 2 }} />
        <Typography variant="h4" component="h1" gutterBottom>
          Authentication Error
        </Typography>
        <Alert severity="error" sx={{ mb: 3, textAlign: "left" }}>
          {message}
        </Alert>
        <Button component={Link} href="/auth/signin" variant="contained">
          Try Again
        </Button>
      </Paper>
    </Container>
  );
}

export default function ErrorPage() {
  return (
    <Suspense
      fallback={
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
            <Typography variant="h6">Loading...</Typography>
          </Paper>
        </Container>
      }
    >
      <ErrorContent />
    </Suspense>
  );
}
