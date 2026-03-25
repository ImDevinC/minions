import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import Link from "next/link";
import { Suspense } from "react";
import { SignOutButton } from "@/components/sign-out-button";
import { MinionCard, MinionCardSkeleton } from "@/components/minion-card";
import { listMinions, OrchestratorError } from "@/lib/orchestrator";

const COMPLETED_PAGE_SIZE = 30;
const COMPLETED_STATUSES = ["completed", "failed", "terminated"];

function parsePositivePage(value?: string | string[]): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const parsed = Number.parseInt(raw ?? "1", 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
}

function buildPageHref(page: number): string {
  return page === 1 ? "/completed" : `/completed?page=${page}`;
}

function PaginationControls({
  currentPage,
  hasNextPage,
}: {
  currentPage: number;
  hasNextPage: boolean;
}) {
  return (
    <div className="flex items-center justify-between mt-6">
      {currentPage > 1 ? (
        <Link
          href={buildPageHref(currentPage - 1)}
          className="px-3 py-2 text-sm rounded-md border border-gray-700 text-gray-200 hover:bg-gray-800"
        >
          Previous
        </Link>
      ) : (
        <span />
      )}
      <span className="text-sm text-gray-400">Page {currentPage}</span>
      {hasNextPage ? (
        <Link
          href={buildPageHref(currentPage + 1)}
          className="px-3 py-2 text-sm rounded-md border border-gray-700 text-gray-200 hover:bg-gray-800"
        >
          Next
        </Link>
      ) : (
        <span />
      )}
    </div>
  );
}

async function CompletedMinionList({ page }: { page: number }) {
  try {
    const offset = (page - 1) * COMPLETED_PAGE_SIZE;
    const minions = await listMinions({
      statuses: COMPLETED_STATUSES,
      limit: COMPLETED_PAGE_SIZE + 1,
      offset,
    });

    const hasNextPage = minions.length > COMPLETED_PAGE_SIZE;
    const pageItems = hasNextPage ? minions.slice(0, COMPLETED_PAGE_SIZE) : minions;

    if (pageItems.length === 0 && page === 1) {
      return <p className="text-gray-500">No completed minions yet.</p>;
    }

    if (pageItems.length === 0) {
      return (
        <div className="text-center py-12 text-gray-400">
          <p className="text-lg">No completed minions on this page</p>
          <Link
            href="/completed"
            className="text-sm mt-2 inline-block text-blue-400 hover:text-blue-300"
          >
            Return to page 1
          </Link>
        </div>
      );
    }

    return (
      <>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {pageItems.map((minion) => (
            <MinionCard key={minion.id} minion={minion} />
          ))}
        </div>
        <PaginationControls currentPage={page} hasNextPage={hasNextPage} />
      </>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <div className="bg-red-900/30 border border-red-700 rounded-lg p-4 text-red-300">
          <p className="font-medium">Failed to load completed minions</p>
          <p className="text-sm mt-1">{error.message}</p>
        </div>
      );
    }
    throw error;
  }
}

function CompletedListSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: 9 }).map((_, i) => (
        <MinionCardSkeleton key={i} />
      ))}
    </div>
  );
}

export default async function CompletedMinionsPage({
  searchParams,
}: {
  searchParams?: { page?: string | string[] };
}) {
  const session = await getServerSession(authOptions);
  if (!session) {
    redirect("/api/auth/signin");
  }

  const page = parsePositivePage(searchParams?.page);

  return (
    <main className="min-h-screen bg-gray-900 text-white p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <h1 className="text-3xl font-bold">Completed Minions</h1>
          <div className="flex items-center gap-4">
            <Link href="/" className="text-gray-400 hover:text-white transition-colors">
              Active Minions
            </Link>
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

        <div className="flex items-center justify-between mb-4">
          <p className="text-sm text-gray-400">
            Showing {COMPLETED_PAGE_SIZE} per page
          </p>
        </div>

        <Suspense fallback={<CompletedListSkeleton />}>
          <CompletedMinionList page={page} />
        </Suspense>
      </div>
    </main>
  );
}
