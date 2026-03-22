import { getServerSession } from "next-auth";
import { NextResponse } from "next/server";
import { authOptions } from "@/lib/auth";
import { terminateMinion, OrchestratorError } from "@/lib/orchestrator";

export async function POST(
  request: Request,
  { params }: { params: { id: string } }
) {
  // Verify user is authenticated (includes CSRF protection via NextAuth)
  const session = await getServerSession(authOptions);
  if (!session) {
    return NextResponse.json(
      { error: "Unauthorized" },
      { status: 401 }
    );
  }

  const { id } = params;

  // Validate ID format (basic UUID check)
  const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  if (!uuidRegex.test(id)) {
    return NextResponse.json(
      { error: "Invalid minion ID format" },
      { status: 400 }
    );
  }

  try {
    const result = await terminateMinion(id);
    return NextResponse.json(result);
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return NextResponse.json(
        { error: error.message },
        { status: error.status }
      );
    }
    console.error("Terminate error:", error);
    return NextResponse.json(
      { error: "Internal server error" },
      { status: 500 }
    );
  }
}
