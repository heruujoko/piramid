import type {
  LoopView,
  FireView,
  GateSummary,
  GateDetail,
  GateDecisionInput,
} from "./types";

const BASE = "/v1";

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "APIError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: { code: "unknown", message: res.statusText } }));
    throw new APIError(res.status, body.error?.code ?? "unknown", body.error?.message ?? res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  listLoops: () => request<LoopView[]>("/loops"),
  listFires: (loopID: string) => request<FireView[]>(`/loops/${loopID}/fires`),
  listOpenGates: () => request<GateSummary[]>("/gates"),
  getGate: (id: string) => request<GateDetail>(`/gates/${id}`),
  resolveGate: (id: string, input: GateDecisionInput) =>
    request<void>(`/gates/${id}/decision`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
};
