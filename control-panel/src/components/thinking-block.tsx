"use client";

import { useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import Collapse from "@mui/material/Collapse";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";

interface ThinkingBlockProps {
  content: string;
}

export function ThinkingBlock({ content }: ThinkingBlockProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!content) {
    return null;
  }

  return (
    <Box sx={{ mb: 2 }}>
      <Button
        size="small"
        color="inherit"
        onClick={() => setIsExpanded(!isExpanded)}
        startIcon={isExpanded ? <ExpandMoreIcon /> : <ChevronRightIcon />}
        sx={{ textTransform: "none", color: "text.secondary" }}
      >
        Thinking...
      </Button>
      <Collapse in={isExpanded}>
        <Box
          sx={{
            mt: 1,
            pl: 2,
            borderLeft: 2,
            borderColor: "divider",
          }}
        >
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}
          >
            {content}
          </Typography>
        </Box>
      </Collapse>
    </Box>
  );
}
