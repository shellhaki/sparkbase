"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details: Record<string, unknown> = {},
  ) {
    super(message);
  }
}

/** Calls the Sparkbase API through the dashboard's /api proxy. */
export async function api<T = any>(method: string, path: string, body?: unknown, init: { raw?: boolean } = {}): Promise<T> {
  const headers: Record<string, string> = {};
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    if (init.raw) {
      payload = body as BodyInit;
    } else {
      headers["Content-Type"] = "application/json";
      payload = JSON.stringify(body);
    }
  }
  const res = await fetch("/api" + path, { method, headers, body: payload, credentials: "same-origin" });
  const text = await res.text();
  let data: any;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    data = text;
  }
  if (!res.ok) {
    const e = data?.error;
    throw new ApiError(res.status, e?.code ?? `http_${res.status}`, e?.message ?? `Request failed (${res.status})`, e?.details);
  }
  return data as T;
}

/** Fetches a path and re-fetches on reload() or when the path changes. */
export function useApi<T = any>(path: string | null, opts: { poll?: number } = {}) {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);

  const load = useCallback(async () => {
    if (!path) return;
    const n = ++seq.current;
    try {
      const d = await api<T>("GET", path);
      if (n === seq.current) {
        setData(d);
        setError(null);
      }
    } catch (e) {
      if (n === seq.current) setError(e as ApiError);
    } finally {
      if (n === seq.current) setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  useEffect(() => {
    if (!opts.poll) return;
    const t = setInterval(load, opts.poll);
    return () => clearInterval(t);
  }, [load, opts.poll]);

  return { data, error, loading, reload: load, setData };
}

export function formatBytes(n: number | undefined | null): string {
  if (n === undefined || n === null) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

export function timeAgo(iso: string | null | undefined): string {
  if (!iso) return "never";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

export const KIND_LABEL: Record<string, string> = {
  postgres: "Postgres",
  cache: "Cache",
  storage: "Storage",
  vector: "Vector",
  queue: "Queues",
  auth: "Auth",
};

export const KIND_BLURB: Record<string, string> = {
  postgres: "A Postgres database with its own role. Native connection string plus SQL over HTTP.",
  cache: "A Redis namespace with its own ACL user. Native rediss:// plus a key/value API.",
  storage: "A private bucket for files. Upload and download through the API or signed URLs.",
  vector: "Collections of embeddings with similarity search (pgvector).",
  queue: "Durable queues with visibility timeouts, retries and dead letters.",
  auth: "Email and password sign-in for your app's users, with JWT sessions.",
};
