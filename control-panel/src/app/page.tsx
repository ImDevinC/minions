import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect } from "next/navigation";
import { MinionCard, MinionCardSkeleton } from "@/components/minion-card";
import { listMinions, OrchestratorError } from "@/lib/orchestrator";
import { Suspense } from "react";
import { AppHeader } from "@/components/app-header";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Grid from "@mui/material/Grid";
import Alert from "@mui/material/Alert";
import MailIcon from "@mui/icons-material/Mail";
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

function buildHref(activePage: number, completedPage: number): string {
  const params = new URLSearchParams();
  if (activePage > 1) params.set("activePage", String(activePage));
  if (completedPage > 1) params.set("completedPage", String(completedPage));
  const qs = params.toString();
  return qs ? `/?${qs}` : "/";
}

interface PaginationControlsProps {
  currentPage: number;
  hasNextPage: boolean;
  buildHref: (page: number) => string;
}

function PaginationControls({ currentPage, hasNextPage, buildHref }: PaginationControlsProps) {
  if (currentPage <= 1 && !hasNextPage) return null;

  return (
    <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", mt: 3 }}>
      <Button
        component={Link}
        href={buildHref(currentPage - 1)}
        disabled={currentPage <= 1}
        variant="outlined"
        size="small"
      >
        Previous
      </Button>
      <Typography variant="body2" color="text.secondary">
        Page {currentPage}
      </Typography>
      <Button
        component={Link}
        href={buildHref(currentPage + 1)}
        disabled={!hasNextPage}
        variant="outlined"
        size="small"
      >
        Next
      </Button>
    </Box>
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
        <Box sx={{ textAlign: "center", py: 8 }}>
          <MailIcon sx={{ fontSize: 64, color: "text.disabled", mb: 2 }} />
          <Typography variant="h6" color="text.secondary">
            No active minions
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Spawn one from Discord with @minion --repo owner/repo &lt;task&gt;
          </Typography>
        </Box>
      );
    }

    if (pageItems.length === 0) {
      return (
        <Box sx={{ textAlign: "center", py: 8 }}>
          <Typography variant="h6" color="text.secondary">
            No active minions on this page
          </Typography>
          <PaginationControls
            currentPage={page}
            hasNextPage={hasNextPage}
            buildHref={() => buildHref(1, completedPage)}
          />
        </Box>
      );
    }

    return (
      <>
        <Grid container spacing={2}>
          {pageItems.map((minion) => (
            <Grid key={minion.id} size={{ xs: 12, md: 6, lg: 4 }}>
              <MinionCard minion={minion} />
            </Grid>
          ))}
        </Grid>
        <PaginationControls
          currentPage={page}
          hasNextPage={hasNextPage}
          buildHref={(p) => buildHref(p, completedPage)}
        />
      </>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <Alert severity="error">
          <Typography variant="subtitle2">Failed to load active minions</Typography>
          <Typography variant="body2">{error.message}</Typography>
        </Alert>
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
    const pageItems = hasNextPage ? minions.slice(0, COMPLETED_PAGE_SIZE) : minions;
    if (pageItems.length === 0 && page === 1) {
      return (
        <Typography variant="body2" color="text.secondary">
          No completed minions yet.
        </Typography>
      );
    }

    if (pageItems.length === 0) {
      return (
        <Box sx={{ textAlign: "center", py: 8 }}>
          <Typography variant="h6" color="text.secondary">
            No completed minions on this page
          </Typography>
          <PaginationControls
            currentPage={page}
            hasNextPage={hasNextPage}
            buildHref={() => buildHref(activePage, 1)}
          />
        </Box>
      );
    }

    return (
      <>
        <Grid container spacing={2}>
          {pageItems.map((minion) => (
            <Grid key={minion.id} size={{ xs: 12, md: 6, lg: 4 }}>
              <MinionCard minion={minion} />
            </Grid>
          ))}
        </Grid>
        <PaginationControls
          currentPage={page}
          hasNextPage={hasNextPage}
          buildHref={(p) => buildHref(activePage, p)}
        />
      </>
    );
  } catch (error) {
    if (error instanceof OrchestratorError) {
      return (
        <Alert severity="error">
          <Typography variant="subtitle2">Failed to load completed minions</Typography>
          <Typography variant="body2">{error.message}</Typography>
        </Alert>
      );
    }
    throw error;
  }
}

function ActiveListSkeleton() {
  return (
    <Grid container spacing={2}>
      {Array.from({ length: ACTIVE_PAGE_SIZE }).map((_, i) => (
        <Grid key={i} size={{ xs: 12, md: 6, lg: 4 }}>
          <MinionCardSkeleton />
        </Grid>
      ))}
    </Grid>
  );
}

function CompletedListSkeleton() {
  return (
    <Grid container spacing={2}>
      {Array.from({ length: COMPLETED_PAGE_SIZE }).map((_, i) => (
        <Grid key={i} size={{ xs: 12, md: 6, lg: 4 }}>
          <MinionCardSkeleton />
        </Grid>
      ))}
    </Grid>
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
    <>
      <AppHeader />
      <Container maxWidth="lg">
        <Box sx={{ mb: 6 }}>
          <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", mb: 2 }}>
            <Typography variant="h5" component="h2">
              Active Minions
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Showing {ACTIVE_PAGE_SIZE} per page
            </Typography>
          </Box>
          <Suspense fallback={<ActiveListSkeleton />}>
            <ActiveMinionList page={activePage} completedPage={completedPage} />
          </Suspense>
        </Box>

        <Box>
          <Box sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", mb: 2 }}>
            <Typography variant="h5" component="h2">
              Completed Minions
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Showing {COMPLETED_PAGE_SIZE} per page
            </Typography>
          </Box>
          <Suspense fallback={<CompletedListSkeleton />}>
            <CompletedMinionList page={completedPage} activePage={activePage} />
          </Suspense>
        </Box>
      </Container>
    </>
  );
}
