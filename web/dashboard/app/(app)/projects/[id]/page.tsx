"use client";

import { Boxes, Plus, RotateCw, Trash2 } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { PageHeader } from "@/components/shell";
import { Badge, Button, Card, CardHeader, CodeBlock, ConfirmModal, CopyField, Empty, ErrorText, Field, Input, Modal, Select, Spinner, StatusDot, cn } from "@/components/ui";
import { KIND_BLURB, KIND_LABEL, api, useApi } from "@/lib/api";
import { KIND_ICON, KINDS, type Resource } from "@/lib/kinds";

interface Project {
  id: string;
  ref: string;
  name: string;
  status: string;
  url: string;
}

interface Plan {
  id: string;
  kind: string;
  name: string;
  limits: Record<string, number>;
  isDefault: boolean;
}

function planSummary(p: Plan): string {
  const l = p.limits;
  const parts: string[] = [];
  if (l.storage_mb) parts.push(l.storage_mb >= 1024 ? `${l.storage_mb / 1024} GB` : `${l.storage_mb} MB`);
  if (l.memory_mb) parts.push(`${l.memory_mb} MB memory`);
  if (l.max_users) parts.push(l.max_users < 0 ? "unlimited users" : `${l.max_users.toLocaleString()} users`);
  if (l.direct_connections) parts.push(`${l.direct_connections} connections`);
  return parts.join(" · ");
}

function AddServiceModal({ open, onClose, project, onAdded, hasAuth }: { open: boolean; onClose: () => void; project: Project; onAdded: () => void; hasAuth: boolean }) {
  const [kind, setKind] = useState("postgres");
  const [name, setName] = useState("");
  const [plan, setPlan] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const { data: plans } = useApi<{ plans: Plan[] }>(open ? `/v1/plans?kind=${kind}` : null);

  useEffect(() => {
    setName(kind === "auth" ? "auth" : kind === "postgres" ? "main" : kind === "storage" ? "files" : kind === "cache" ? "cache" : kind === "queue" ? "jobs" : "embeddings");
    setPlan("");
    setError(null);
  }, [kind]);

  return (
    <Modal open={open} onClose={onClose} title="Add a service" wide>
      <form
        className="space-y-5"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          try {
            await api("POST", `/v1/projects/${project.id}/resources`, { kind, name, plan: plan || undefined });
            onAdded();
            onClose();
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="grid gap-2 sm:grid-cols-2">
          {KINDS.map((k) => {
            const Icon = KIND_ICON[k];
            const disabled = k === "auth" && hasAuth;
            return (
              <button
                type="button"
                key={k}
                disabled={disabled}
                onClick={() => setKind(k)}
                className={cn(
                  "flex gap-3 rounded-lg border p-3 text-left transition-colors disabled:opacity-40",
                  kind === k ? "border-brand bg-brand/5" : "border-border hover:bg-accent/50",
                )}
              >
                <Icon className="mt-0.5 size-5 shrink-0 text-brand" />
                <span>
                  <span className="block text-sm font-medium">{KIND_LABEL[k]}</span>
                  <span className="block text-xs text-muted-foreground">{disabled ? "This project already has auth." : KIND_BLURB[k]}</span>
                </span>
              </button>
            );
          })}
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" hint="Lowercase letters, digits and dashes.">
            <Input required pattern="[a-z]([a-z0-9-]*[a-z0-9])?" maxLength={31} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Plan">
            <Select value={plan} onChange={(e) => setPlan(e.target.value)}>
              {plans?.plans.map((p) => (
                <option key={p.id} value={p.isDefault ? "" : p.id}>
                  {p.name} — {planSummary(p)}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <ErrorText error={error} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" loading={busy}>
            Add {KIND_LABEL[kind]}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function ConnectCard({ project }: { project: Project }) {
  const { data, reload } = useApi<{ url: string; publishableKey: string; secretKey: string }>(`/v1/projects/${project.id}/keys`);
  const [rotating, setRotating] = useState<"publishable" | "secret" | null>(null);
  if (!data) return null;
  return (
    <Card>
      <CardHeader title="Connect your app" description="Use the project URL and a key with the SDK or plain HTTP." />
      <div className="grid gap-4 p-5 lg:grid-cols-2">
        <div className="space-y-4">
          <CopyField label="Project URL" value={data.url} />
          <div className="flex items-end gap-2">
            <div className="min-w-0 flex-1">
              <CopyField label="Publishable key — safe in browsers, auth routes only" value={data.publishableKey} />
            </div>
            <Button variant="ghost" size="sm" onClick={() => setRotating("publishable")} aria-label="Rotate publishable key">
              <RotateCw className="size-4" />
            </Button>
          </div>
          <div className="flex items-end gap-2">
            <div className="min-w-0 flex-1">
              <CopyField label="Secret key — server only, full access" value={data.secretKey} secret />
            </div>
            <Button variant="ghost" size="sm" onClick={() => setRotating("secret")} aria-label="Rotate secret key">
              <RotateCw className="size-4" />
            </Button>
          </div>
        </div>
        <CodeBlock
          code={`npm install @sparkbase/sdk

import { createClient } from '@sparkbase/sdk'

const sb = createClient(
  process.env.SPARKBASE_URL,        // ${data.url}
  process.env.SPARKBASE_SECRET_KEY,
)

# or pull everything into .env:
sparkbase link ${project.ref} && sparkbase env pull`}
        />
      </div>
      <ConfirmModal
        open={!!rotating}
        onClose={() => setRotating(null)}
        title={`Rotate the ${rotating} key`}
        body="The current key stops working immediately. Update every app that uses it."
        actionLabel="Rotate key"
        onConfirm={async () => {
          await api("POST", `/v1/projects/${project.id}/keys/rotate`, { kind: rotating });
          await reload();
        }}
      />
    </Card>
  );
}

export default function ProjectPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState("");
  const { data, error, loading, reload } = useApi<{ project: Project; resources: Resource[] }>(`/v1/projects/${id}`);
  const provisioning = data?.resources.some((r) => r.status === "provisioning" || r.status === "deleting");

  useEffect(() => {
    if (!provisioning) return;
    const t = setInterval(reload, 1500);
    return () => clearInterval(t);
  }, [provisioning, reload]);

  if (loading) return <Spinner />;
  if (error || !data) return <ErrorText error={error ?? "Project not found"} />;
  const { project, resources } = data;

  return (
    <>
      <PageHeader
        back={<Link href="/">← Projects</Link>}
        title={project.name}
        description={<span className="font-mono">{project.ref}</span>}
        action={
          <div className="flex gap-2">
            <Button
              variant="secondary"
              onClick={() => {
                setNewName(project.name);
                setRenaming(true);
              }}
            >
              Rename
            </Button>
            <Button variant="danger" onClick={() => setDeleting(true)}>
              <Trash2 className="size-4" /> Delete
            </Button>
          </div>
        }
      />
      {project.status === "deleting" && <ErrorText error="This project is being deleted." />}

      <div className="space-y-8">
        <ConnectCard project={project} />

        <section>
          <div className="mb-4 flex items-center justify-between">
            <h2 className="font-display text-lg font-semibold">Services</h2>
            <Button onClick={() => setAdding(true)}>
              <Plus className="size-4" /> Add service
            </Button>
          </div>
          {resources.length === 0 ? (
            <Card>
              <Empty title="No services yet" action={<Button onClick={() => setAdding(true)}>Add your first service</Button>}>
                Add only what your app needs. Each service gets its own credentials and limits.
              </Empty>
            </Card>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {resources.map((r) => {
                const Icon = KIND_ICON[r.kind] ?? Boxes;
                return (
                  <Link key={r.id} href={`/projects/${project.id}/services/${r.id}`} className="group">
                    <Card className="h-full p-5 transition-colors group-hover:border-brand/50">
                      <div className="flex items-center gap-3">
                        <div className="rounded-lg bg-brand/10 p-2">
                          <Icon className="size-5 text-brand" />
                        </div>
                        <div className="min-w-0">
                          <div className="truncate font-medium">{r.name}</div>
                          <div className="text-xs text-muted-foreground">{KIND_LABEL[r.kind]}</div>
                        </div>
                      </div>
                      <div className="mt-5 flex items-center justify-between text-xs">
                        <span className="flex items-center gap-1.5">
                          <StatusDot status={r.status} /> {r.status}
                          {r.writeLocked && <Badge tone="paused">read-only</Badge>}
                        </span>
                        <span className="text-muted-foreground">{r.plan.replace(`${r.kind}-`, "")}</span>
                      </div>
                    </Card>
                  </Link>
                );
              })}
            </div>
          )}
        </section>
      </div>

      <AddServiceModal open={adding} onClose={() => setAdding(false)} project={project} onAdded={reload} hasAuth={resources.some((r) => r.kind === "auth")} />
      <ConfirmModal
        open={deleting}
        onClose={() => setDeleting(false)}
        title="Delete project"
        body={`This deletes ${project.name} and every service in it, including all data. This cannot be undone.`}
        confirmWord={project.ref}
        actionLabel="Delete project"
        onConfirm={async () => {
          await api("DELETE", `/v1/projects/${project.id}`);
          router.replace("/");
        }}
      />
      <Modal open={renaming} onClose={() => setRenaming(false)} title="Rename project">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            await api("PATCH", `/v1/projects/${project.id}`, { name: newName });
            setRenaming(false);
            reload();
          }}
        >
          <Field label="Name">
            <Input value={newName} onChange={(e) => setNewName(e.target.value)} maxLength={60} required autoFocus />
          </Field>
          <div className="flex justify-end">
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Modal>
    </>
  );
}
