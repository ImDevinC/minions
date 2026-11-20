import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { getStats, listMinions, OrchestratorError } from "@/lib/orchestrator";
import Link from "next/link";
import { Suspense } from "react";
import { AppHeader } from "@/components/app-header";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Chip from "@mui/material/Chip";
import Alert from "@mui/material/Alert";
import Skeleton from "@mui/material/Skeleton";
import Button from "@mui/material/Button";

const MINION_PAGE_SIZE = 20;

function parsePositivePage(value?: string | string[]): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const parsed = Number.parseInt(raw ?? "1", 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
}

function minionPageHref(page: number): string {
  return page <= 1 ? "/stats" : `/stats?minionPage=${page}`;
}

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

function StatusChip({ status }: { status: string }) {
  const colors: Record<string, "default" | "primary" | "success" | "error" | "warning"> = {
    pending: "default",
    awaiting_clarification: "warning",
    running: "primary",
    completed: "success",
    failed: "error",
    terminated: "warning",
  };
  return <Chip size="small" color={colors[status] || "default"} label={status} />;
}

function StatCard({ label, value, subtext }: { label: string; value: string; subtext?: string }) {
  return (
    <Paper elevation={2} sx={{ p: 3, height: "100%" }}>
      <Typography variant="body2" color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: 1 }}>
        {label}
      </Typography>
      <Typography variant="h4" sx={{ mt: 1 }}>
        {value}
      </Typography>
      {subtext && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          {subtext}
        </Typography>
      )}
    </Paper>
  );
}

async function StatsContent({ page }: { page: number }) {
  try {
    const [stats, minions] = await Promise.all([
      getStats(),
      listMinions({
        limit: MINION_PAGE_SIZE + 1,
        offset: (page - 1) * MINION_PAGE_SIZE,
      }),
    ]);

    // The orchestrator returns minions newest-first; keep that order instead of sorting by cost.
    const hasNextPage = minions.length > MINION_PAGE_SIZE;
    const minionsWithCost = minions
      .slice(0, MINION_PAGE_SIZE)
      .filter((minion) => minion.cost_usd > 0);

    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <section>
          <Typography variant="h5" component="h2" gutterBottom>
            Total Usage
          </Typography>
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 4 }}>
              <StatCard label="Total Cost" value={formatCost(stats.total_cost_usd)} />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <StatCard label="Input Tokens" value={formatTokens(stats.total_input_tokens)} />
            </Grid>
            <Grid size={{ xs: 12, md: 4 }}>
              <StatCard label="Output Tokens" value={formatTokens(stats.total_output_tokens)} />
            </Grid>
          </Grid>
        </section>

        <section>
          <Typography variant="h5" component="h2" gutterBottom>
            Breakdown by Model
          </Typography>
          {stats.by_model.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No usage data yet
            </Typography>
          ) : (
            <TableContainer component={Paper}>
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>Model</TableCell>
                    <TableCell align="right">Cost</TableCell>
                    <TableCell align="right">Input</TableCell>
                    <TableCell align="right">Output</TableCell>
                    <TableCell align="right">Runs</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {stats.by_model.map((model) => (
                    <TableRow key={model.model} hover>
                      <TableCell>
                        <Box
                          component="code"
                          sx={{
                            px: 1,
                            py: 0.5,
                            borderRadius: 1,
                            backgroundColor: "rgba(255,255,255,0.05)",
                            fontSize: "0.875rem",
                          }}
                        >
                          {model.model}
                        </Box>
                      </TableCell>
                      <TableCell align="right" sx={{ color: "success.main" }}>
                        {formatCost(model.cost_usd)}
                      </TableCell>
                      <TableCell align="right">{formatTokens(model.input_tokens)}</TableCell>
                      <TableCell align="right">{formatTokens(model.output_tokens)}</TableCell>
                      <TableCell align="right">{model.count}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </section>

        <section>
          <Typography variant="h5" component="h2" gutterBottom>
            Per-Minion Costs
          </Typography>
          {minionsWithCost.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              {page === 1 ? "No minions with costs yet" : "No minions with costs on this page"}
            </Typography>
          ) : (
            <TableContainer component={Paper}>
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>Minion</TableCell>
                    <TableCell>Model</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell align="right">Cost</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {minionsWithCost.map((minion) => (
                    <TableRow key={minion.id} hover>
                      <TableCell>
                        <Typography
                          component={Link}
                          href={`/minions/${minion.id}`}
                          color="primary.main"
                          sx={{ textDecoration: "none", "&:hover": { textDecoration: "underline" } }}
                        >
                          {minion.repo}
                        </Typography>
                        <Typography variant="body2" color="text.secondary" noWrap sx={{ maxWidth: 300 }}>
                          {minion.task.slice(0, 50)}
                          {minion.task.length > 50 ? "..." : ""}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        <Box
                          component="code"
                          sx={{
                            px: 1,
                            py: 0.5,
                            borderRadius: 1,
                            backgroundColor: "rgba(255,255,255,0.05)",
                            fontSize: "0.875rem",
                          }}
                        >
                          {minion.model}
                        </Box>
                      </TableCell>
                      <TableCell>
                        <StatusChip status={minion.status} />
                      </TableCell>
                      <TableCell align="right" sx={{ color: "success.main" }}>
                        {formatCost(minion.cost_usd)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          )}
          {(page > 1 || hasNextPage) && (
            <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", mt: 2 }}>
              <Button
                component={Link}
                href={minionPageHref(page - 1)}
                disabled={page <= 1}
                variant="outlined"
                size="small"
              >
                Previous
              </Button>
              <Typography variant="body2" color="text.secondary">
                Page {page}
              </Typography>
              <Button
                component={Link}
                href={minionPageHref(page + 1)}
                disabled={!hasNextPage}
                variant="outlined"
                size="small"
              >
                Next
              </Button>
            </Box>
          )}
        </section>
      </Box>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <Alert severity="error">
          <Typography variant="subtitle2">Failed to load stats</Typography>
          <Typography variant="body2">{error.message}</Typography>
        </Alert>
      );
    }
    throw error;
  }
}

function StatsLoading() {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <section>
        <Skeleton width={140} height={32} sx={{ mb: 2 }} />
        <Grid container spacing={2}>
          {[1, 2, 3].map((i) => (
            <Grid key={i} size={{ xs: 12, md: 4 }}>
              <Paper sx={{ p: 3 }}>
                <Skeleton width={80} height={16} />
                <Skeleton width={120} height={40} sx={{ mt: 1 }} />
              </Paper>
            </Grid>
          ))}
        </Grid>
      </section>
      <section>
        <Skeleton width={160} height={32} sx={{ mb: 2 }} />
        <Skeleton variant="rectangular" height={200} sx={{ borderRadius: 1 }} />
      </section>
    </Box>
  );
}

export default async function StatsPage({
  searchParams,
}: {
  searchParams?: { minionPage?: string | string[] };
}) {
  const session = await getServerSession(authOptions);

  if (!session) {
    redirect("/api/auth/signin");
  }

  return (
    <>
      <AppHeader title="Statistics" backHref="/" backLabel="Dashboard" />
      <Container maxWidth="lg" sx={{ pb: 4 }}>
        <Suspense fallback={<StatsLoading />}>
          <StatsContent page={parsePositivePage(searchParams?.minionPage)} />
        </Suspense>
      </Container>
    </>
  );
}
