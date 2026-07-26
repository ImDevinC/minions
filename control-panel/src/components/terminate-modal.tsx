"use client";

import { useState, useCallback, useEffect } from "react";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import CircularProgress from "@mui/material/CircularProgress";
import WarningIcon from "@mui/icons-material/Warning";

interface TerminateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => Promise<void>;
  minionId: string;
  repo: string;
}

export function TerminateModal({
  isOpen,
  onClose,
  onConfirm,
  minionId,
  repo,
}: TerminateModalProps) {
  const [isTerminating, setIsTerminating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleConfirm = useCallback(async () => {
    setIsTerminating(true);
    setError(null);
    try {
      await onConfirm();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to terminate minion");
    } finally {
      setIsTerminating(false);
    }
  }, [onConfirm, onClose]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !isTerminating) {
        onClose();
      }
    };
    if (isOpen) {
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }
  }, [isOpen, isTerminating, onClose]);

  return (
    <Dialog open={isOpen} onClose={isTerminating ? undefined : onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <WarningIcon color="error" />
        Terminate Minion?
      </DialogTitle>
      <DialogContent>
        <Typography variant="body1" gutterBottom>
          Are you sure you want to terminate this minion? This action cannot be undone.
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
          Repository: <strong>{repo}</strong>
        </Typography>
        <Typography variant="caption" color="text.disabled" sx={{ display: "block",  mt: 0.5  }}>
          ID: {minionId.slice(0, 8)}...
        </Typography>
        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={isTerminating}>
          Cancel
        </Button>
        <Button
          onClick={handleConfirm}
          disabled={isTerminating}
          color="error"
          variant="contained"
          startIcon={isTerminating ? <CircularProgress size={16} color="inherit" /> : undefined}
        >
          {isTerminating ? "Terminating..." : "Terminate"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
