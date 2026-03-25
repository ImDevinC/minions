import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { SignOutButton } from "@/components/sign-out-button";
import { MinionCard, MinionCardSkeleton } from "@/components/minion-card";
import { listMinions, OrchestratorError } from "@/lib/orchestrator";
import { Suspense } from "react";
import Link from "next/link";

const ACTIVE_PAGE_SIZE = 9;
const ACTIVE_STATUSES = ["pending", "running", "awaiting_clarification"];

const COMPLETED_PAGE_SIZE = 30;
const COMPLETED_STATUSES = ["completed", "failed", "terminated"];

function parsePositivePage(value?: string | string[]): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const parsed = Number.parseInt(raw ?? "1", 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
}

function buildActivePageHref(
  page: number,
  completedPage: number
): string {
  const params = new URLSearchParams();
  if (page > 1) params.set("activePage", String(page));
  if (completedPage > 1) params.set("completedPage", String(completedPage));
  const qs = params.toString();
  return qs ? `/?${qs}` : "/";
}

function buildCompletedPageHref(
  activePage: number,
  page: number
): string {
  const params = new URLSearchParams();
  if (activePage > 1) params.set("activePage", String(activePage));
  if (page > 1) params.set("completedPage", String(page));
  const qs = params.toString();
  return qs ? `/?${qs}` : "/";
}

function PaginationControls({
  currentPage,
  hasNextPage,
  buildHref,
}: {
  currentPage: number;
  hasNextPage: boolean;
  buildHref: (page: number) => string;
}) {
  return (
    <div className="flex items-center justify-between mt-6">
      {currentPage > 1 ? (
        <Link
          href={buildHref(currentPage - 1)}
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
          href={buildHref(currentPage + 1)}
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

async function ActiveMinionList({
  page,
  completedPage,
}: {
  page: number;
  completedPage: number;
}) {
  try {
    const offset = (page - 1) * ACTIVE_PAGE_SIZE;
    const minions = await listMinions({
      statuses: ACTIVE_STATUSES,
      limit: ACTIVE_PAGE_SIZE + 1,
      offset,
    });

    const hasNextPage = minions.length > ACTIVE_PAGE_SIZE;
    const pageItems = hasNextPage ? minions.slice(0, ACTIVE_PAGE_SIZE) : minions;

    if (pageItems.length === 0 && page === 1) {
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
          <p className="text-lg">No active minions</p>
          <p className="text-sm mt-1">
            Spawn one from Discord with @minion --repo owner/repo &lt;task&gt;
          </p>
        </div>
      );
    }

    if (pageItems.length === 0) {
      return (
        <div className="text-center py-12 text-gray-400">
          <p className="text-lg">No active minions on this page</p>
          <Link
            href={buildActivePageHref(1, completedPage)}
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
        <PaginationControls
          currentPage={page}
          hasNextPage={hasNextPage}
          buildHref={(p) => buildActivePageHref(p, completedPage)}
        />
      </>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <div className="bg-red-900/30 border border-red-700 rounded-lg p-4 text-red-300">
          <p className="font-medium">Failed to load active minions</p>
          <p className="text-sm mt-1">{error.message}</p>
        </div>
      );
    }
    throw error;
  }
}

async function CompletedMinionList({
  page,
  activePage,
}: {
  page: number;
  activePage: number;
}) {
  try {
    const offset = (page - 1) * COMPLETED_PAGE_SIZE;
    const minions = await listMinions({
      statuses: COMPLETED_STATUSES,
      limit: COMPLETED_PAGE_SIZE + 1,
      offset,
    });

    const hasNextPage = minions.length > COMPLETED_PAGE_SIZE;
    const pageItems = hasNextPage
      ? minions.slice(0, COMPLETED_PAGE_SIZE)
      : minions;

    if (pageItems.length === 0 && page === 1) {
      return <p className="text-gray-500">No completed minions yet.</p>;
    }

    if (pageItems.length === 0) {
      return (
        <div className="text-center py-12 text-gray-400">
          <p className="text-lg">No completed minions on this page</p>
          <Link
            href={buildCompletedPageHref(activePage, 1)}
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
        <PaginationControls
          currentPage={page}
          hasNextPage={hasNextPage}
          buildHref={(p) => buildCompletedPageHref(activePage, p)}
        />
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

function ActiveListSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: ACTIVE_PAGE_SIZE }).map((_, i) => (
        <MinionCardSkeleton key={i} />
      ))}
    </div>
  );
}

function CompletedListSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {Array.from({ length: COMPLETED_PAGE_SIZE }).map((_, i) => (
        <MinionCardSkeleton key={i} />
      ))}
    </div>
  );
}

export default async function Home({
  searchParams,
}: {
  searchParams?: {
    activePage?: string | string[];
    completedPage?: string | string[];
  };
}) {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  const activePage = parsePositivePage(searchParams?.activePage);
  const completedPage = parsePositivePage(searchParams?.completedPage);

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

        <section className="mb-12">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold text-gray-100">
              Active Minions
            </h2>
            <span className="text-sm text-gray-400">
              Showing {ACTIVE_PAGE_SIZE} per page
            </span>
          </div>
          <Suspense fallback={<ActiveListSkeleton />}>
            <ActiveMinionList page={activePage} completedPage={completedPage} />
          </Suspense>
        </section>

        <section>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold text-gray-100">
              Completed Minions
            </h2>
            <span className="text-sm text-gray-400">
              Showing {COMPLETED_PAGE_SIZE} per page
            </span>
          </div>
          <Suspense fallback={<CompletedListSkeleton />}>
            <CompletedMinionList page={completedPage} activePage={activePage} />
          </Suspense>
        </section>
      </div>
    </main>
  );
}
