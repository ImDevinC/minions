import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect, notFound } from "next/navigation";
import Link from "next/link";
import { getMinion, OrchestratorError } from "@/lib/orchestrator";
import { MinionDetailClient } from "@/components/minion-detail-client";

export default async function MinionDetailPage({
  params,
}: {
  params: { id: string };
}) {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  try {
    const minion = await getMinion(params.id);

    return (
      <main className="min-h-screen bg-gray-900 text-white p-8">
        <div className="max-w-6xl mx-auto">
          <Link
            href="/"
            className="inline-flex items-center gap-2 text-gray-400 hover:text-white mb-6 transition-colors"
          >
            <svg
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M15 19l-7-7 7-7"
              />
            </svg>
            Back to Dashboard
          </Link>

          <MinionDetailClient minion={minion} />
        </div>
      </main>
    );
  } catch (error) {
    if (error instanceof OrchestratorError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
