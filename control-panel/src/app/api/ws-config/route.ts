import { getServerSession } from "next-auth";
import { NextResponse } from "next/server";
import { authOptions } from "@/lib/auth";

// Returns WebSocket configuration for the control panel
// Only returns the token if the user is authenticated
export async function GET() {
  const session = await getServerSession(authOptions);
  if (!session) {
    return NextResponse.json({ error: "Unauthorized" }, { status: 401 });
  }

  const orchestratorUrl = process.env.ORCHESTRATOR_URL;
  const token = process.env.INTERNAL_API_TOKEN;

  if (!orchestratorUrl || !token) {
    return NextResponse.json(
      { error: "WebSocket configuration not available" },
      { status: 500 }
    );
  }

  // Convert HTTP URL to WebSocket URL
  const wsUrl = orchestratorUrl
    .replace(/^https:/, "wss:")
    .replace(/^http:/, "ws:");

  return NextResponse.json({
    wsUrl,
    token,
  });
}
