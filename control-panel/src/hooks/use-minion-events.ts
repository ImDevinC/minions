"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { MinionEvent, MinionStatus } from "@/types/minion";

// WebSocket reconnection constants
const INITIAL_BACKOFF_MS = 1000;
const MAX_BACKOFF_MS = 60000;
const BACKOFF_MULTIPLIER = 2;

interface UseMinionEventsOptions {
  minionId: string;
  initialEvents: MinionEvent[];
  status: MinionStatus;
}

interface UseMinionEventsResult {
  events: MinionEvent[];
  isConnected: boolean;
  connectionError: string | null;
}

interface WSConfig {
  wsUrl: string;
  token: string;
}

// Fetch WebSocket configuration from the server
async function fetchWSConfig(): Promise<WSConfig | null> {
  try {
    const response = await fetch("/api/ws-config");
    if (!response.ok) {
      console.error("Failed to fetch WS config:", response.status);
      return null;
    }
    return response.json();
  } catch (err) {
    console.error("Error fetching WS config:", err);
    return null;
  }
}

// Fetch events since a timestamp (for reconnection catch-up)
async function fetchEventsSince(
  minionId: string,
  since: string
): Promise<MinionEvent[]> {
  const response = await fetch(
    `/api/minions/${minionId}/events?since=${encodeURIComponent(since)}`
  );
  if (!response.ok) {
    throw new Error(`Failed to fetch events: ${response.status}`);
  }
  const data = await response.json();
  return data.events || [];
}

// Merge and deduplicate events by ID
function mergeEvents(
  existing: MinionEvent[],
  newEvents: MinionEvent[]
): MinionEvent[] {
  const eventMap = new Map<string, MinionEvent>();

  // Add existing events
  for (const event of existing) {
    eventMap.set(event.id, event);
  }

  // Add/update with new events
  for (const event of newEvents) {
    eventMap.set(event.id, event);
  }

  // Sort by timestamp ascending
  return Array.from(eventMap.values()).sort(
    (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
  );
}

// Get the latest timestamp from a list of events
function getLatestTimestamp(events: MinionEvent[]): string | null {
  if (events.length === 0) return null;

  let latest = events[0].timestamp;
  for (const event of events) {
    if (new Date(event.timestamp).getTime() > new Date(latest).getTime()) {
      latest = event.timestamp;
    }
  }
  return latest;
}

// Check if minion is in a terminal state (no more events expected)
function isTerminalStatus(status: MinionStatus): boolean {
  return (
    status === "completed" || status === "failed" || status === "terminated"
  );
}

export function useMinionEvents({
  minionId,
  initialEvents,
  status,
}: UseMinionEventsOptions): UseMinionEventsResult {
  const [events, setEvents] = useState<MinionEvent[]>(initialEvents);
  const [isConnected, setIsConnected] = useState(false);
  const [connectionError, setConnectionError] = useState<string | null>(null);

  // Refs for WebSocket reconnection logic
  const wsRef = useRef<WebSocket | null>(null);
  const wsConfigRef = useRef<WSConfig | null>(null);
  const backoffRef = useRef(INITIAL_BACKOFF_MS);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const lastTimestampRef = useRef<string | null>(
    getLatestTimestamp(initialEvents)
  );
  const mountedRef = useRef(true);
  const currentStatusRef = useRef(status);

  // Update status ref when it changes
  useEffect(() => {
    currentStatusRef.current = status;
  }, [status]);

  // Update lastTimestamp when events change
  useEffect(() => {
    const latest = getLatestTimestamp(events);
    if (latest) {
      lastTimestampRef.current = latest;
    }
  }, [events]);

  const connect = useCallback(async () => {
    if (!mountedRef.current) return;

    // Don't connect if status is terminal
    if (isTerminalStatus(currentStatusRef.current)) {
      return;
    }

    // Fetch WS config if we don't have it
    if (!wsConfigRef.current) {
      const config = await fetchWSConfig();
      if (!config) {
        setConnectionError("Failed to get WebSocket configuration");
        // Retry after backoff
        const backoff = backoffRef.current;
        backoffRef.current = Math.min(
          backoff * BACKOFF_MULTIPLIER,
          MAX_BACKOFF_MS
        );
        reconnectTimeoutRef.current = setTimeout(() => {
          if (mountedRef.current) {
            connect();
          }
        }, backoff);
        return;
      }
      wsConfigRef.current = config;
    }

    const config = wsConfigRef.current;
    const wsUrl = `${config.wsUrl}/api/minions/${minionId}/stream?token=${encodeURIComponent(config.token)}`;

    try {
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        if (!mountedRef.current) {
          ws.close();
          return;
        }

        setIsConnected(true);
        setConnectionError(null);
        backoffRef.current = INITIAL_BACKOFF_MS;

        // On reconnect, fetch missed events
        const lastTs = lastTimestampRef.current;
        if (lastTs) {
          fetchEventsSince(minionId, lastTs)
            .then((missedEvents) => {
              if (!mountedRef.current) return;
              if (missedEvents.length > 0) {
                setEvents((prev) => mergeEvents(prev, missedEvents));
              }
            })
            .catch((err) => {
              console.error("Failed to fetch missed events:", err);
            });
        }
      };

      ws.onmessage = (event) => {
        if (!mountedRef.current) return;

        try {
          const data = JSON.parse(event.data);
          // WebSocket sends individual events
          const newEvent: MinionEvent = {
            id: data.id,
            timestamp: data.timestamp,
            event_type: data.event_type,
            content: data.content,
          };
          setEvents((prev) => mergeEvents(prev, [newEvent]));
        } catch (err) {
          console.error("Failed to parse WebSocket message:", err);
        }
      };

      ws.onerror = () => {
        setConnectionError("WebSocket connection error");
      };

      ws.onclose = (event) => {
        if (!mountedRef.current) return;

        setIsConnected(false);
        wsRef.current = null;

        // Don't reconnect if terminal status or normal close
        if (
          isTerminalStatus(currentStatusRef.current) ||
          event.code === 1000
        ) {
          return;
        }

        // Schedule reconnect with exponential backoff
        const backoff = backoffRef.current;
        backoffRef.current = Math.min(
          backoff * BACKOFF_MULTIPLIER,
          MAX_BACKOFF_MS
        );

        reconnectTimeoutRef.current = setTimeout(() => {
          if (mountedRef.current) {
            connect();
          }
        }, backoff);
      };
    } catch (err) {
      console.error("Failed to create WebSocket:", err);
      setConnectionError("Failed to create WebSocket connection");

      // Schedule retry
      const backoff = backoffRef.current;
      backoffRef.current = Math.min(
        backoff * BACKOFF_MULTIPLIER,
        MAX_BACKOFF_MS
      );
      reconnectTimeoutRef.current = setTimeout(() => {
        if (mountedRef.current) {
          connect();
        }
      }, backoff);
    }
  }, [minionId]);

  // Connect on mount, cleanup on unmount
  useEffect(() => {
    mountedRef.current = true;

    // Only connect for non-terminal statuses
    if (!isTerminalStatus(status)) {
      connect();
    }

    return () => {
      mountedRef.current = false;

      // Cleanup WebSocket
      if (wsRef.current) {
        wsRef.current.close(1000, "Component unmounting");
        wsRef.current = null;
      }

      // Cancel any pending reconnect
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
    };
  }, [connect, status]);

  // If status transitions to terminal, close connection
  useEffect(() => {
    if (isTerminalStatus(status) && wsRef.current) {
      wsRef.current.close(1000, "Minion completed");
      wsRef.current = null;
    }
  }, [status]);

  return {
    events,
    isConnected,
    connectionError,
  };
}
