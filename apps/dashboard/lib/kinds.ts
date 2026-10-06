export interface Resource {
  id: string;
  projectId: string;
  projectRef: string;
  kind: string;
  name: string;
  plan: string;
  limits: Record<string, number>;
  status: string;
  writeLocked: boolean;
  lastError: string | null;
  lastActiveAt: string | null;
  createdAt: string;
  apiUrl: string;
  runtime?: "shared" | "container" | "control";
  awake?: boolean;
  settings?: Record<string, unknown>;
}

export const DATA_KINDS = ["postgres", "cache", "storage", "vector", "queue", "auth"];
export const PLATFORM_KINDS = ["realtime", "cron", "backup", "monitor"];
export const KINDS = [...DATA_KINDS, ...PLATFORM_KINDS];

/** Kinds that run in their own container when the engine is on. */
export const CONTAINER_KINDS = ["postgres", "vector", "queue", "auth", "cache"];

/** Project API path for a service, through the dashboard proxy. */
export function servicePath(r: Resource): string {
  return r.kind === "auth" ? `/p/${r.projectRef}/auth` : `/p/${r.projectRef}/${r.kind}/${r.name}`;
}
