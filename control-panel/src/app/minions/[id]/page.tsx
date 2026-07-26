import { getServerSession } from "next-auth";
import { authOptions } from "@/lib/auth";
import { redirect, notFound } from "next/navigation";
import { getMinion, OrchestratorError } from "@/lib/orchestrator";
import { MinionDetailClient } from "@/components/minion-detail-client";
import { AppHeader } from "@/components/app-header";
import Container from "@mui/material/Container";

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
      <>
        <AppHeader title="Minion Detail" backHref="/" backLabel="Dashboard" />
        <Container maxWidth="lg" sx={{ pb: 4 }}>
          <MinionDetailClient minion={minion} />
        </Container>
      </>
    );
  } catch (error) {
    if (error instanceof OrchestratorError && error.status === 404) {
      notFound();
    }
    throw error;
  }
}
