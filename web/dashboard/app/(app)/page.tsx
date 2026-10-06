"use client";

import { ArrowRight, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader } from "@/components/shell";
import { Button, Card, Empty, ErrorText, Field, Input, Modal, Spinner } from "@/components/ui";
import { api, timeAgo, useApi } from "@/lib/api";

interface Project {
  id: string;
  ref: string;
  name: string;
  status: string;
  url: string;
  createdAt: string;
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
      <PageHeader title="Projects" description="Each project has its own URL, keys and services." action={create} />
      <ErrorText error={error} />
      {loading ? (
        <Spinner />
      ) : data && data.projects.length === 0 ? (
        <Card>
          <Empty title="No projects yet" action={create}>
            Create a project, then add the services your app needs: Postgres, cache, storage, vector, queues or auth.
          </Empty>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data?.projects.map((p) => (
            <Link key={p.id} href={`/projects/${p.id}`} className="group">
              <Card className="h-full p-5 transition-colors group-hover:border-brand/50">
                <div className="flex items-start justify-between gap-2">
                  <h2 className="font-display font-semibold">{p.name}</h2>
                  <ArrowRight className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                </div>
                <p className="mt-1 font-mono text-xs text-muted-foreground">{p.ref}</p>
                <p className="mt-6 text-xs text-muted-foreground">
                  {p.status === "deleting" ? "Deleting…" : `Created ${timeAgo(p.createdAt)}`}
                </p>
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
          <Field label="Project name">
            <Input required maxLength={60} value={name} onChange={(e) => setName(e.target.value)} placeholder="My app" autoFocus />
          </Field>
          <ErrorText error={createError} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" loading={busy}>
              Create project
            </Button>
          </div>
        </form>
      </Modal>
    </>
  );
}
