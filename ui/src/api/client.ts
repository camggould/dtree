import { useAppStore } from "@/store/app";
import type { ProblemDetails } from "@/api/types.gen";

export class ApiError extends Error {
  readonly status: number;
  readonly problem: ProblemDetails;

  constructor(problem: ProblemDetails) {
    super(problem.detail || problem.title);
    this.name = "ApiError";
    this.status = problem.status;
    this.problem = problem;
  }
}

export interface ApiResponse<T> {
  data: T;
  etag: string | null;
}

/**
 * Core fetch wrapper. Reads X-Dtree-As from the persisted store,
 * handles RFC 7807 error bodies, and returns {data, etag}.
 */
export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<ApiResponse<T>> {
  const handle = useAppStore.getState().currentHandle;

  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (handle) {
    headers.set("X-Dtree-As", handle);
  }

  const res = await fetch(path, { ...init, headers });

  if (!res.ok) {
    let problem: ProblemDetails;
    const contentType = res.headers.get("content-type") ?? "";
    if (contentType.includes("application/problem+json") || contentType.includes("application/json")) {
      try {
        problem = (await res.json()) as ProblemDetails;
      } catch {
        problem = {
          type: "about:blank",
          title: res.statusText,
          status: res.status,
          detail: `HTTP ${res.status} ${res.statusText}`,
        };
      }
    } else {
      problem = {
        type: "about:blank",
        title: res.statusText,
        status: res.status,
        detail: `HTTP ${res.status} ${res.statusText}`,
      };
    }
    throw new ApiError(problem);
  }

  const etag = res.headers.get("etag");

  // Handle 204 No Content
  if (res.status === 204) {
    return { data: undefined as unknown as T, etag };
  }

  const data = (await res.json()) as T;
  return { data, etag };
}
