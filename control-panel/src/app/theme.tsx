"use client";

import { ReactNode } from "react";
import { createTheme, ThemeProvider } from "@mui/material/styles";
import CssBaseline from "@mui/material/CssBaseline";
import "@fontsource/roboto/300.css";
import "@fontsource/roboto/400.css";
import "@fontsource/roboto/500.css";
import "@fontsource/roboto/700.css";

const theme = createTheme({
  palette: {
    mode: "dark",
    background: {
      default: "#0f172a",
      paper: "#1e293b",
    },
    primary: {
      main: "#60a5fa",
    },
    secondary: {
      main: "#a78bfa",
    },
    success: {
      main: "#4ade80",
    },
    warning: {
      main: "#facc15",
    },
    error: {
      main: "#f87171",
    },
  },
  typography: {
    fontFamily: "Roboto, Helvetica, Arial, sans-serif",
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          backgroundColor: "#0f172a",
          color: "#f8fafc",
        },
      },
    },
  },
});

export function AppTheme({ children }: { children: ReactNode }) {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      {children}
    </ThemeProvider>
  );
}
