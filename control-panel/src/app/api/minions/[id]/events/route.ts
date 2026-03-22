import { getServerSession } from "next-auth";
import { NextResponse } from "next/server";
import { authOptions } from "@/lib/auth";
import { getEventsSince, OrchestratorError } from "@/lib/orchestrator";

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

  // Get 'since' parameter from query string
  const url = new URL(request.url);
  const since = url.searchParams.get("since");

  if (!since) {
    return NextResponse.json(
      { error: "Missing 'since' parameter" },
      { status: 400 }
    );
  }

  // Validate timestamp format (basic ISO8601 check)
  const timestamp = new Date(since);
  if (isNaN(timestamp.getTime())) {
    return NextResponse.json(
      { error: "Invalid timestamp format" },
      { status: 400 }
    );
  }

  try {
    const events = await getEventsSince(id, { since });
    return NextResponse.json({ events });
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return NextResponse.json({ error: error.message }, { status: error.status });
    }
    console.error("Get events error:", error);
    return NextResponse.json({ error: "Internal server error" }, { status: 500 });
  }
}
