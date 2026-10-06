"use client";

import { ArrowUpRight, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Greeting } from "@/components/greeting";
import { ServiceLogo } from "@/components/logos/service-logo";
import { useSession } from "@/components/shell";
import { Button, ErrorText, Field, Input, Modal, Skeleton } from "@/components/ui";
import { KIND_LABEL, api, timeAgo, useApi } from "@/lib/api";
import { KINDS } from "@/lib/kinds";

interface Project {
  id: string;
  ref: string;
  name: string;
  status: string;
  url: string;
  createdAt: string;
  serviceKinds?: string[];
}

/** A thin bar split by service kind, in each kind's color. */
function SignalStrip({ kinds, className = "" }: { kinds: string[]; className?: string }) {
  if (kinds.length === 0) return <div className={`h-1 rounded-full bg-muted ${className}`} />;
  return (
    <div className={`flex h-1 gap-0.5 overflow-hidden rounded-full ${className}`}>
      {kinds.map((k, i) => (
        <span key={i} className="flex-1 rounded-full bg-brand" style={{ opacity: 1 - (i % 3) * 0.25 }} />
      ))}
    </div>
  );
}

function ProjectPanel({ p, i }: { p: Project; i: number }) {
  const kinds = p.serviceKinds ?? [];
  return (
    <Link href={`/projects/${p.id}`} className="group animate-fade-up block" style={{ animationDelay: `${80 + i * 45}ms` }}>
      <div className="lift relative h-full overflow-hidden rounded-2xl border border-border bg-card/80 p-5 backdrop-blur-sm group-hover:border-brand/40">
        <SignalStrip kinds={kinds} />
        <div className="mt-5 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="truncate font-display text-lg font-semibold tracking-tight">{p.name}</h2>
            <span className="mt-1 inline-block rounded-md bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">{p.ref}</span>
          </div>
          <span className="flex size-8 shrink-0 items-center justify-center rounded-full border border-border text-muted-foreground transition-colors group-hover:border-brand/50 group-hover:bg-brand group-hover:text-on-brand">
            <ArrowUpRight className="size-4" />
          </span>
        </div>
        <div className="mt-8 flex items-end justify-between gap-3">
          {kinds.length > 0 ? (
            <div className="flex gap-1.5">
              {kinds.slice(0, 6).map((k, j) => (
                <ServiceLogo key={j} kind={k} size="sm" />
              ))}
              {kinds.length > 6 && <span className="flex size-8 items-center justify-center rounded-lg bg-muted font-mono text-xs">+{kinds.length - 6}</span>}
            </div>
          ) : (
            <span className="text-sm text-muted-foreground">Empty, add a service</span>
          )}
          <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{p.status === "deleting" ? "deleting…" : timeAgo(p.createdAt)}</span>
        </div>
      </div>
    </Link>
  );
}

export default function HomePage() {
  const router = useRouter();
  const { user } = useSession();
  const { data, error, loading } = useApi<{ projects: Project[] }>("/v1/projects");
  const { data: status } = useApi<{ healthy: boolean }>("/v1/status", { poll: 30_000 });
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [createError, setCreateError] = useState<unknown>(null);

  // The command palette opens this dialog with ?new=1.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("new") === "1") {
      setOpen(true);
      router.replace("/");
    }
  }, [router]);

  const projects = data?.projects ?? [];
  const allKinds = projects.flatMap((p) => p.serviceKinds ?? []);
  const byKind = KINDS.map((k) => ({ k, n: allKinds.filter((x) => x === k).length })).filter((x) => x.n > 0);
  const firstName = (user.name ?? user.email.split("@")[0]).split(" ")[0];

  return (
    <>
      <section className="relative mb-12">
        <div className="relative flex flex-wrap items-end justify-between gap-6">
          <Greeting name={firstName} />
          <div className="flex items-center gap-2 rounded-full border border-border bg-card/70 px-3 py-1.5 font-mono text-xs backdrop-blur">
            <span className={`led ${status?.healthy === false ? "text-warning" : "text-success"}`} data-live="true" />
            {status?.healthy === false ? "a data host needs attention" : "all systems normal"}
          </div>
        </div>

        <div className="relative mt-10 grid gap-3 sm:grid-cols-3">
          {[
            { label: "Projects", value: loading ? null : projects.length },
            { label: "Services", value: loading ? null : allKinds.length },
          ].map((s, i) => (
            <div key={s.label} className="animate-fade-up rounded-2xl border border-border bg-card/70 p-5 backdrop-blur-sm" style={{ animationDelay: `${i * 50}ms` }}>
              <div className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">{s.label}</div>
              {s.value === null ? <Skeleton className="mt-3 h-9 w-14" /> : <div className="tabular mt-2 font-display text-4xl font-semibold tracking-tight">{s.value}</div>}
            </div>
          ))}
          <div className="animate-fade-up rounded-2xl border border-border bg-card/70 p-5 backdrop-blur-sm" style={{ animationDelay: "100ms" }}>
            <div className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">Service mix</div>
            {byKind.length === 0 ? (
              <p className="mt-4 text-sm text-muted-foreground">No services yet</p>
            ) : (
              <>
                <div className="mt-4 flex h-2.5 gap-0.5 overflow-hidden rounded-full">
                  {byKind.map(({ k, n }) => (
                    <span key={k} className="bg-brand first:opacity-100 even:opacity-60" style={{ flex: n }} title={`${KIND_LABEL[k]}: ${n}`} />
                  ))}
                </div>
                <div className="mt-3 flex flex-wrap gap-x-3 gap-y-1">
                  {byKind.map(({ k, n }) => (
                    <span key={k} className="flex items-center gap-1.5 text-xs text-muted-foreground">
                      <span className="size-1.5 rounded-full bg-brand" />
                      {KIND_LABEL[k]} <span className="tabular font-mono text-foreground">{n}</span>
                    </span>
                  ))}
                </div>
              </>
            )}
          </div>
        </div>
      </section>

      <div className="mb-5 flex items-center justify-between">
        <h2 className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">Your projects</h2>
        {projects.length > 0 && (
          <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
            <Plus className="size-3.5" /> New project
          </Button>
        )}
      </div>
      <ErrorText error={error} />
      {loading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-48 rounded-2xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {projects.map((p, i) => (
            <ProjectPanel key={p.id} p={p} i={i} />
          ))}
          <button
            onClick={() => setOpen(true)}
            className="animate-fade-up group flex min-h-48 flex-col items-center justify-center gap-4 rounded-2xl border border-dashed border-border p-6 text-muted-foreground transition-colors hover:border-brand/50 hover:bg-card/60 hover:text-foreground"
            style={{ animationDelay: `${80 + projects.length * 45}ms` }}
          >
            <span className="flex gap-1.5 opacity-70 transition-opacity group-hover:opacity-100">
              {KINDS.map((k) => (
                <ServiceLogo key={k} kind={k} size="sm" />
              ))}
            </span>
            <span className="flex items-center gap-1.5 text-sm font-medium">
              <Plus className="size-4" /> {projects.length === 0 ? "Create your first project" : "New project"}
            </span>
          </button>
        </div>
      )}

      <Modal open={open} onClose={() => setOpen(false)} title="New project">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setCreateError(null);
            try {
              const d = await api<{ project: Project }>("POST", "/v1/projects", { name });
              router.push(`/projects/${d.project.id}`);
            } catch (err) {
              setCreateError(err);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Name">
            <Input required maxLength={60} value={name} onChange={(e) => setName(e.target.value)} placeholder="My app" autoFocus />
          </Field>
          <ErrorText error={createError} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" loading={busy}>
              Create
            </Button>
          </div>
        </form>
      </Modal>
    </>
  );
}
