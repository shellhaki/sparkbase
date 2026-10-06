"use client";

import { ArrowUpRight, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ServiceLogo } from "@/components/logos/service-logo";
import { PageHeader } from "@/components/shell";
import { Button, Card, Empty, ErrorText, Field, Input, Modal, SkeletonCards } from "@/components/ui";
import { KINDS } from "@/lib/kinds";
import { api, timeAgo, useApi } from "@/lib/api";

interface Project {
  id: string;
  ref: string;
  name: string;
  status: string;
  url: string;
  createdAt: string;
  serviceKinds: string[];
}

export default function ProjectsPage() {
  const router = useRouter();
  const { data, error, loading } = useApi<{ projects: Project[] }>("/v1/projects");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [createError, setCreateError] = useState<unknown>(null);

  const create = (
    <Button onClick={() => setOpen(true)}>
      <Plus className="size-4" /> New project
    </Button>
  );

  return (
    <>
      <PageHeader title="Projects" action={data && data.projects.length > 0 && create} />
      <ErrorText error={error} />
      {loading ? (
        <SkeletonCards count={3} />
      ) : data && data.projects.length === 0 ? (
        <Card className="animate-fade-up overflow-hidden">
          <div className="flex flex-col items-center gap-6 px-6 py-14 text-center">
            <div className="flex gap-2">
              {KINDS.map((k, i) => (
                <span key={k} className="animate-fade-up" style={{ animationDelay: `${i * 60}ms` }}>
                  <ServiceLogo kind={k} />
                </span>
              ))}
            </div>
            <div>
              <p className="font-display text-lg font-semibold">Start your first project</p>
              <p className="mt-1 text-sm text-muted-foreground">Add a database, cache, storage, vectors, queues or auth.</p>
            </div>
            {create}
          </div>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data?.projects.map((p, i) => (
            <Link key={p.id} href={`/projects/${p.id}`} className="group animate-fade-up" style={{ animationDelay: `${i * 50}ms` }}>
              <Card className="card-hover flex h-full flex-col p-5">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <h2 className="truncate font-display font-semibold">{p.name}</h2>
                    <p className="mt-0.5 font-mono text-xs text-muted-foreground">{p.ref}</p>
                  </div>
                  <ArrowUpRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-foreground" />
                </div>
                <div className="mt-6 flex items-end justify-between gap-3">
                  {p.serviceKinds.length > 0 ? (
                    <div className="flex gap-1.5">
                      {p.serviceKinds.slice(0, 6).map((k, j) => (
                        <ServiceLogo key={j} kind={k} size="sm" />
                      ))}
                      {p.serviceKinds.length > 6 && (
                        <span className="flex size-8 items-center justify-center rounded-lg bg-muted text-xs ring-2 ring-card">+{p.serviceKinds.length - 6}</span>
                      )}
                    </div>
                  ) : (
                    <span className="text-xs text-muted-foreground">No services</span>
                  )}
                  <span className="shrink-0 text-xs text-muted-foreground">{p.status === "deleting" ? "Deleting…" : timeAgo(p.createdAt)}</span>
                </div>
              </Card>
            </Link>
          ))}
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
