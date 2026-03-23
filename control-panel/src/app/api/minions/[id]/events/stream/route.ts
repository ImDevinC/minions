import { getServerSession } from "next-auth";
import { NextResponse } from "next/server";
import { authOptions } from "@/lib/auth";

const ORCHESTRATOR_URL = process.env.ORCHESTRATOR_URL;
const INTERNAL_API_TOKEN = process.env.INTERNAL_API_TOKEN;

// SSE heartbeat interval (30 seconds)
const HEARTBEAT_INTERVAL_MS = 30000;

export async function GET(
  request: Request,
  { params }: { params: { id: string } }
) {
  // Verify user is authenticated
  const session = await getServerSession(authOptions);
  if (!session) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const { id } = params;

  // Validate ID format (basic UUID check)
  const uuidRegex =
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  if (!uuidRegex.test(id)) {
    return NextResponse.json(
      { error: "Invalid minion ID format" },
      { status: 400 }
    );
  }

  if (!ORCHESTRATOR_URL || !INTERNAL_API_TOKEN) {
    return NextResponse.json(
      { error: "Orchestrator configuration not available" },
      { status: 500 }
    );
  }

  // Convert HTTP URL to WebSocket URL
  const wsUrl = ORCHESTRATOR_URL
    .replace(/^https:/, "wss:")
    .replace(/^http:/, "ws:");

  const orchestratorWsUrl = `${wsUrl}/api/minions/${id}/stream?token=${encodeURIComponent(INTERNAL_API_TOKEN)}`;

  // Create SSE stream
  const encoder = new TextEncoder();
  let ws: WebSocket | null = null;
  let heartbeatInterval: NodeJS.Timeout | null = null;

  const stream = new ReadableStream({
    start(controller) {
      try {
        // Connect to orchestrator WebSocket
        ws = new WebSocket(orchestratorWsUrl);

        ws.onopen = () => {
          // Start heartbeat to keep connection alive
          heartbeatInterval = setInterval(() => {
            try {
              controller.enqueue(encoder.encode(": heartbeat\n\n"));
            } catch (err) {
              console.error("Failed to send heartbeat:", err);
            }
          }, HEARTBEAT_INTERVAL_MS);
        };

        ws.onmessage = (event) => {
          try {
            // Parse WebSocket message
            const data = JSON.parse(event.data);

            // Transform to SSE format
            // Use 'message' event type for standard events
            const sseMessage = `event: message\ndata: ${JSON.stringify(data)}\n\n`;
            controller.enqueue(encoder.encode(sseMessage));
          } catch (err) {
            console.error("Failed to process WebSocket message:", err);
          }
        };

        ws.onerror = (error) => {
          console.error("WebSocket error:", error);
          // Send error event to client
          const errorMessage = `event: error\ndata: ${JSON.stringify({ error: "upstream connection failed" })}\n\n`;
          controller.enqueue(encoder.encode(errorMessage));
        };

        ws.onclose = () => {
          // Clean up heartbeat interval
          if (heartbeatInterval) {
            clearInterval(heartbeatInterval);
            heartbeatInterval = null;
          }
          // Close SSE stream
          controller.close();
        };
      } catch (err) {
        console.error("Failed to create WebSocket:", err);
        const errorMessage = `event: error\ndata: ${JSON.stringify({ error: "failed to connect to orchestrator" })}\n\n`;
        controller.enqueue(encoder.encode(errorMessage));
        controller.close();
      }
    },
    cancel() {
      // Browser disconnected - clean up WebSocket
      if (heartbeatInterval) {
        clearInterval(heartbeatInterval);
        heartbeatInterval = null;
      }
      if (ws) {
        ws.close(1000, "Client disconnected");
        ws = null;
      }
    },
  });

  // Return SSE response with proper headers
  return new NextResponse(stream, {
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
