import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect, notFound } from "next/navigation";
import Link from "next/link";
import { getMinion, OrchestratorError } from "@/lib/orchestrator";

// Placeholder page - full implementation in panel-3
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

          <div className="bg-gray-800 border border-gray-700 rounded-lg p-6">
            <h1 className="text-2xl font-bold mb-4">{minion.repo}</h1>
            <p className="text-gray-300 mb-4">{minion.task}</p>
            <div className="text-sm text-gray-500">
              <p>Status: {minion.status}</p>
              <p>Model: {minion.model}</p>
              <p>Created: {new Date(minion.created_at).toLocaleString()}</p>
              {minion.pr_url && (
                <p>
                  PR:{" "}
                  <a
                    href={minion.pr_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-blue-400 hover:underline"
                  >
                    {minion.pr_url}
                  </a>
                </p>
              )}
            </div>
            <p className="mt-6 text-gray-500 text-sm">
              Full detail view with live events coming in panel-3.
            </p>
          </div>
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
