import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { SignOutButton } from "@/components/sign-out-button";
import { MinionCard, MinionCardSkeleton } from "@/components/minion-card";
import { listMinions, OrchestratorError } from "@/lib/orchestrator";
import { Suspense } from "react";
import Link from "next/link";

async function MinionList() {
  try {
    const minions = await listMinions({ limit: 50 });

    if (minions.length === 0) {
      return (
        <div className="text-center py-12 text-gray-400">
          <svg
            className="w-16 h-16 mx-auto mb-4 text-gray-600"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={1.5}
              d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
            />
          </svg>
          <p className="text-lg">No minions yet</p>
          <p className="text-sm mt-1">
            Spawn one from Discord with @minion --repo owner/repo &lt;task&gt;
          </p>
        </div>
      );
    }

    return (
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {minions.map((minion) => (
          <MinionCard key={minion.id} minion={minion} />
        ))}
      </div>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <div className="bg-red-900/30 border border-red-700 rounded-lg p-4 text-red-300">
          <p className="font-medium">Failed to load minions</p>
          <p className="text-sm mt-1">
            {error.message}
          </p>
        </div>
      );
    }
    // Re-throw unexpected errors
    throw error;
  }
}

function MinionListSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <MinionCardSkeleton key={i} />
      ))}
    </div>
  );
}

export default async function Home() {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  return (
    <main className="min-h-screen bg-gray-900 text-white p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <h1 className="text-3xl font-bold">Minions Control Panel</h1>
          <div className="flex items-center gap-4">
            <Link
              href="/stats"
              className="text-gray-400 hover:text-white transition-colors"
            >
              Stats
            </Link>
            <span className="text-gray-400">{session.user?.name}</span>
            <SignOutButton />
          </div>
        </div>

        <Suspense fallback={<MinionListSkeleton />}>
          <MinionList />
        </Suspense>
      </div>
    </main>
  );
}
