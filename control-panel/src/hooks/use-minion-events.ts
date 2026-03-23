"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { MinionEvent, MinionStatus } from "@/types/minion";

// Debounce delay for batching rapid SSE events into single React renders
// Matches ~60fps for smooth UI updates
const EVENT_DEBOUNCE_MS = 16;

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

  // Refs for EventSource lifecycle
  const eventSourceRef = useRef<EventSource | null>(null);
  const lastTimestampRef = useRef<string | null>(
    getLatestTimestamp(initialEvents)
  );
  const mountedRef = useRef(true);
  const currentStatusRef = useRef(status);
  
  // Debounce buffer for batching rapid SSE events
  const pendingEventsRef = useRef<MinionEvent[]>([]);
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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
  
  // Flush pending events to state (used by debounce)
  const flushPendingEvents = useCallback(() => {
    if (!mountedRef.current) return;
    if (pendingEventsRef.current.length === 0) return;
    
    const eventsToFlush = pendingEventsRef.current;
    pendingEventsRef.current = [];
    debounceTimerRef.current = null;
    
    setEvents((prev) => mergeEvents(prev, eventsToFlush));
  }, []);
  
  // Add event to buffer with debounce (batches rapid events into single render)
  const addEventDebounced = useCallback((event: MinionEvent) => {
    pendingEventsRef.current.push(event);
    
    // If no timer pending, start one
    if (!debounceTimerRef.current) {
      debounceTimerRef.current = setTimeout(flushPendingEvents, EVENT_DEBOUNCE_MS);
    }
  }, [flushPendingEvents]);

  const connect = useCallback(async () => {
    if (!mountedRef.current) return;

    // Don't connect if status is terminal
    if (isTerminalStatus(currentStatusRef.current)) {
      return;
    }

    // Construct SSE endpoint URL (same-origin, proxy to orchestrator)
    const sseUrl = `/api/minions/${minionId}/events/stream`;

    try {
      const eventSource = new EventSource(sseUrl);
      eventSourceRef.current = eventSource;

      eventSource.onopen = () => {
        if (!mountedRef.current) {
          eventSource.close();
          return;
        }

        setIsConnected(true);
        setConnectionError(null);

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

      eventSource.onmessage = (event) => {
        if (!mountedRef.current) return;

        try {
          const data = JSON.parse(event.data);
          
          // Defensive: handle both enriched format (id/timestamp at root)
          // and legacy format (nested in content)
          const newEvent: MinionEvent = {
            id: data.id || data.content?.id || crypto.randomUUID(),
            timestamp: data.timestamp || data.content?.timestamp || new Date().toISOString(),
            event_type: data.event_type || data.content?.event_type || data.type || "unknown",
            content: data.content?.content || data.content || {},
          };
          // Use debounced add to batch rapid events into single React render
          addEventDebounced(newEvent);
        } catch (err) {
          console.error("Failed to parse SSE message:", err);
        }
      };

      eventSource.onerror = (err) => {
        if (!mountedRef.current) return;

        console.error("EventSource connection error:", err);
        setIsConnected(false);
        setConnectionError("SSE connection error");

        // Don't manually retry - EventSource auto-retries
        // If terminal status, close connection
        if (isTerminalStatus(currentStatusRef.current)) {
          eventSource.close();
          eventSourceRef.current = null;
        }
      };
    } catch (err) {
      console.error("Failed to create EventSource:", err);
      setConnectionError("Failed to create SSE connection");
    }
  }, [minionId, addEventDebounced]);

  // Connect on mount, cleanup on unmount
  useEffect(() => {
    mountedRef.current = true;

    // Only connect for non-terminal statuses
    if (!isTerminalStatus(status)) {
      connect();
    }

    return () => {
      mountedRef.current = false;

      // Cleanup EventSource
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
        eventSourceRef.current = null;
      }
      
      // Cleanup debounce timer
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
        debounceTimerRef.current = null;
      }
    };
  }, [connect, status]);

  // If status transitions to terminal, close connection
  useEffect(() => {
    if (isTerminalStatus(status) && eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, [status]);

  return {
    events,
    isConnected,
    connectionError,
  };
}
