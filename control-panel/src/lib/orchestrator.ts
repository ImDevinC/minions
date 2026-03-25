// Server-side orchestrator API client
// Used in Server Components and API routes

import { MinionSummary, MinionDetail, MinionEvent, Stats } from "@/types/minion";

const ORCHESTRATOR_URL = process.env.ORCHESTRATOR_URL;
const INTERNAL_API_TOKEN = process.env.INTERNAL_API_TOKEN;

class OrchestratorError extends Error {
  constructor(
    message: string,
    public status: number
  ) {
    super(message);
    this.name = "OrchestratorError";
  }
}

async function fetchOrchestrator<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  if (!ORCHESTRATOR_URL) {
    throw new Error("ORCHESTRATOR_URL is not configured");
  }
  if (!INTERNAL_API_TOKEN) {
    throw new Error("INTERNAL_API_TOKEN is not configured");
  }

  const url = `${ORCHESTRATOR_URL}${path}`;
  const response = await fetch(url, {
    ...options,
    headers: {
      Authorization: `Bearer ${INTERNAL_API_TOKEN}`,
      "Content-Type": "application/json",
      ...options.headers,
    },
    // Disable caching for API calls
    cache: "no-store",
  });

  if (!response.ok) {
    const text = await response.text();
    throw new OrchestratorError(
      `Orchestrator request failed: ${text}`,
      response.status
    );
  }

  return response.json();
}

export interface ListMinionsParams {
  status?: string;
  statuses?: string[];
  limit?: number;
  offset?: number;
}

export async function listMinions(
  params: ListMinionsParams = {}
): Promise<MinionSummary[]> {
  const searchParams = new URLSearchParams();
  if (params.statuses && params.statuses.length > 0) {
    searchParams.set("statuses", params.statuses.join(","));
  } else if (params.status) {
    searchParams.set("status", params.status);
  }
  if (params.limit) {
    searchParams.set("limit", params.limit.toString());
  }
  if (params.offset !== undefined) {
    searchParams.set("offset", params.offset.toString());
  }

  const query = searchParams.toString();
  const path = `/api/minions${query ? `?${query}` : ""}`;
  return fetchOrchestrator<MinionSummary[]>(path);
}

export async function getMinion(id: string): Promise<MinionDetail> {
  return fetchOrchestrator<MinionDetail>(`/api/minions/${id}`);
}

export async function terminateMinion(id: string): Promise<{ success: boolean }> {
  return fetchOrchestrator<{ success: boolean }>(`/api/minions/${id}`, {
    method: "DELETE",
  });
}

export async function getStats(): Promise<Stats> {
  return fetchOrchestrator<Stats>("/api/stats");
}

export interface GetEventsParams {
  since: string; // ISO8601 timestamp
  limit?: number;
}

interface GetEventsResponse {
  events: MinionEvent[];
}

export async function getEventsSince(
  id: string,
  params: GetEventsParams
): Promise<MinionEvent[]> {
  const searchParams = new URLSearchParams();
  searchParams.set("since", params.since);
  if (params.limit) {
    searchParams.set("limit", params.limit.toString());
  }
  const response = await fetchOrchestrator<GetEventsResponse>(
    `/api/minions/${id}/events?${searchParams.toString()}`
  );
  return response.events;
}

export { OrchestratorError };
