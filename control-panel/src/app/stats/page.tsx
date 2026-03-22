import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getStats, listMinions, OrchestratorError } from "@/lib/orchestrator";
import Link from "next/link";
import { SignOutButton } from "@/components/sign-out-button";
import { Suspense } from "react";

function formatCost(cost: number): string {
  return `$${cost.toFixed(4)}`;
}

function formatTokens(tokens: number): string {
  if (tokens >= 1_000_000) {
    return `${(tokens / 1_000_000).toFixed(2)}M`;
  }
  if (tokens >= 1_000) {
    return `${(tokens / 1_000).toFixed(1)}K`;
  }
  return tokens.toString();
}

function StatCard({
  label,
  value,
  subtext,
}: {
  label: string;
  value: string;
  subtext?: string;
}) {
  return (
    <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
      <p className="text-gray-400 text-sm uppercase tracking-wide">{label}</p>
      <p className="text-3xl font-bold text-white mt-2">{value}</p>
      {subtext && <p className="text-gray-500 text-sm mt-1">{subtext}</p>}
    </div>
  );
}

async function StatsContent() {
  try {
    const [stats, minions] = await Promise.all([
      getStats(),
      listMinions({ limit: 100 }),
    ]);

    // Filter minions with costs (completed with actual usage)
    const minionsWithCost = minions
      .filter((m) => m.cost_usd > 0)
      .sort((a, b) => b.cost_usd - a.cost_usd);

    return (
      <div className="space-y-8">
        {/* Totals */}
        <section>
          <h2 className="text-xl font-semibold mb-4 text-gray-300">
            Total Usage
          </h2>
          <div className="grid gap-4 md:grid-cols-3">
            <StatCard
              label="Total Cost"
              value={formatCost(stats.total_cost_usd)}
            />
            <StatCard
              label="Input Tokens"
              value={formatTokens(stats.total_input_tokens)}
            />
            <StatCard
              label="Output Tokens"
              value={formatTokens(stats.total_output_tokens)}
            />
          </div>
        </section>

        {/* By Model */}
        <section>
          <h2 className="text-xl font-semibold mb-4 text-gray-300">
            Breakdown by Model
          </h2>
          {stats.by_model.length === 0 ? (
            <p className="text-gray-500">No usage data yet</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-gray-700">
                    <th className="text-left py-3 px-4 text-gray-400 font-medium">
                      Model
                    </th>
                    <th className="text-right py-3 px-4 text-gray-400 font-medium">
                      Cost
                    </th>
                    <th className="text-right py-3 px-4 text-gray-400 font-medium">
                      Input
                    </th>
                    <th className="text-right py-3 px-4 text-gray-400 font-medium">
                      Output
                    </th>
                    <th className="text-right py-3 px-4 text-gray-400 font-medium">
                      Runs
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {stats.by_model.map((model) => (
                    <tr
                      key={model.model}
                      className="border-b border-gray-800 hover:bg-gray-800/50"
                    >
                      <td className="py-3 px-4">
                        <code className="text-sm bg-gray-700/50 px-2 py-1 rounded">
                          {model.model}
                        </code>
                      </td>
                      <td className="text-right py-3 px-4 font-mono text-green-400">
                        {formatCost(model.cost_usd)}
                      </td>
                      <td className="text-right py-3 px-4 text-gray-300">
                        {formatTokens(model.input_tokens)}
                      </td>
                      <td className="text-right py-3 px-4 text-gray-300">
                        {formatTokens(model.output_tokens)}
                      </td>
                      <td className="text-right py-3 px-4 text-gray-300">
                        {model.count}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        {/* Per-minion costs */}
        <section>
          <h2 className="text-xl font-semibold mb-4 text-gray-300">
            Per-Minion Costs
          </h2>
          {minionsWithCost.length === 0 ? (
            <p className="text-gray-500">No completed minions with costs yet</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b border-gray-700">
                    <th className="text-left py-3 px-4 text-gray-400 font-medium">
                      Minion
                    </th>
                    <th className="text-left py-3 px-4 text-gray-400 font-medium">
                      Model
                    </th>
                    <th className="text-left py-3 px-4 text-gray-400 font-medium">
                      Status
                    </th>
                    <th className="text-right py-3 px-4 text-gray-400 font-medium">
                      Cost
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {minionsWithCost.map((minion) => (
                    <tr
                      key={minion.id}
                      className="border-b border-gray-800 hover:bg-gray-800/50"
                    >
                      <td className="py-3 px-4">
                        <Link
                          href={`/minions/${minion.id}`}
                          className="text-blue-400 hover:text-blue-300 hover:underline"
                        >
                          {minion.repo}
                        </Link>
                        <p className="text-gray-500 text-sm truncate max-w-xs">
                          {minion.task.slice(0, 50)}
                          {minion.task.length > 50 ? "..." : ""}
                        </p>
                      </td>
                      <td className="py-3 px-4">
                        <code className="text-sm bg-gray-700/50 px-2 py-1 rounded">
                          {minion.model}
                        </code>
                      </td>
                      <td className="py-3 px-4">
                        <StatusBadge status={minion.status} />
                      </td>
                      <td className="text-right py-3 px-4 font-mono text-green-400">
                        {formatCost(minion.cost_usd)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <div className="bg-red-900/30 border border-red-700 rounded-lg p-4 text-red-300">
          <p className="font-medium">Failed to load stats</p>
          <p className="text-sm mt-1">{error.message}</p>
        </div>
      );
    }
    throw error;
  }
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    pending: "bg-gray-600 text-gray-200",
    awaiting_clarification: "bg-yellow-600 text-yellow-100",
    running: "bg-blue-600 text-blue-100",
    completed: "bg-green-600 text-green-100",
    failed: "bg-red-600 text-red-100",
    terminated: "bg-orange-600 text-orange-100",
  };

  return (
    <span
      className={`px-2 py-1 rounded text-xs font-medium ${colors[status] || "bg-gray-600"}`}
    >
      {status}
    </span>
  );
}

function StatsLoading() {
  return (
    <div className="space-y-8">
      <section>
        <div className="h-6 w-32 bg-gray-700 rounded animate-pulse mb-4" />
        <div className="grid gap-4 md:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <div
              key={i}
              className="bg-gray-800 rounded-lg p-6 border border-gray-700"
            >
              <div className="h-4 w-24 bg-gray-700 rounded animate-pulse" />
              <div className="h-8 w-32 bg-gray-700 rounded animate-pulse mt-2" />
            </div>
          ))}
        </div>
      </section>
      <section>
        <div className="h-6 w-40 bg-gray-700 rounded animate-pulse mb-4" />
        <div className="h-48 bg-gray-800 rounded-lg animate-pulse" />
      </section>
    </div>
  );
}

export default async function StatsPage() {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  return (
    <main className="min-h-screen bg-gray-900 text-white p-8">
      <div className="max-w-6xl mx-auto">
        <div className="flex justify-between items-center mb-8">
          <div className="flex items-center gap-4">
            <Link
              href="/"
              className="text-gray-400 hover:text-white transition-colors"
            >
              &larr; Dashboard
            </Link>
            <h1 className="text-3xl font-bold">Statistics</h1>
          </div>
          <div className="flex items-center gap-4">
            <span className="text-gray-400">{session.user?.name}</span>
            <SignOutButton />
          </div>
        </div>

        <Suspense fallback={<StatsLoading />}>
          <StatsContent />
        </Suspense>
      </div>
    </main>
  );
}
