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
}

export const KINDS = ["postgres", "cache", "storage", "vector", "queue", "auth"];

/** Project API path for a service, through the dashboard proxy. */
export function servicePath(r: Resource): string {
  return r.kind === "auth" ? `/p/${r.projectRef}/auth` : `/p/${r.projectRef}/${r.kind}/${r.name}`;
}
