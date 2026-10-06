"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AuthBrowser } from "@/components/browsers/auth";
import { CacheBrowser } from "@/components/browsers/cache";
import { PostgresBrowser } from "@/components/browsers/postgres";
import { QueueBrowser } from "@/components/browsers/queue";
import { StorageBrowser } from "@/components/browsers/storage";
import { VectorBrowser } from "@/components/browsers/vector";
import { PageHeader } from "@/components/shell";
import { Badge, Button, Card, CardHeader, CodeBlock, ConfirmModal, CopyField, ErrorText, Field, Input, Meter, Select, Spinner, StatusDot, Table, Tabs, Td } from "@/components/ui";
import { KIND_LABEL, api, formatBytes, timeAgo, useApi } from "@/lib/api";
import { KIND_ICON, type Resource } from "@/lib/kinds";

type Tab = "overview" | "data" | "backups" | "settings";

interface Connection {
  apiUrl: string;
  projectUrl: string;
  native?: Record<string, string | number | boolean>;
  bucket?: string;
  backend?: string;
  jwtSecret?: string;
}

function snippet(r: Resource): string {
  const head = `import { createClient } from '@sparkbase/sdk'\nconst sb = createClient(process.env.SPARKBASE_URL, process.env.SPARKBASE_SECRET_KEY)\n\n`;
  switch (r.kind) {
    case "postgres":
      return head + `const { rows } = await sb.postgres('${r.name}').query('select now()')`;
    case "cache":
      return head + `await sb.cache('${r.name}').set('user:1', { name: 'Ada' }, { ttl: 60 })\nconst user = await sb.cache('${r.name}').getJSON('user:1')`;
    case "storage":
      return head + `await sb.storage('${r.name}').upload('avatars/1.png', file)\nconst url = await sb.storage('${r.name}').signedUrl('avatars/1.png', { expiresIn: 600 })`;
    case "vector":
      return (
        head +
        `const docs = sb.vector('${r.name}')\nawait docs.createCollection({ name: 'pages', dimension: 1536 })\nawait docs.collection('pages').upsert([{ id: 'p1', vector: embedding, metadata: { url } }])\nconst hits = await docs.collection('pages').query(queryEmbedding, { topK: 5 })`
      );
    case "queue":
      return (
        head +
        `const q = sb.queue('${r.name}')\nawait q.createQueue({ name: 'emails' })\nawait q.queue('emails').send({ to: 'ada@example.com' })\n\nfor (const msg of await q.queue('emails').receive({ max: 10 })) {\n  await sendEmail(msg.body)\n  await q.queue('emails').ack(msg.id)\n}`
      );
    case "auth":
      return `// In the browser, with the publishable key\nconst sb = createClient(process.env.NEXT_PUBLIC_SPARKBASE_URL, process.env.NEXT_PUBLIC_SPARKBASE_PUBLISHABLE_KEY, { storage: localStorage })\n\nawait sb.auth.signUp({ email, password })\nawait sb.auth.signInWithPassword({ email, password })\nconst user = await sb.auth.getUser()\nconst token = await sb.auth.getAccessToken() // send to your server, verify with the JWT secret`;
  }
  return "";
}

const USAGE_ROWS: Record<string, Array<{ metric: string; limit: string; label: string; bytes?: boolean; mb?: boolean }>> = {
  postgres: [
    { metric: "storage_bytes", limit: "storage_mb", label: "Storage", bytes: true, mb: true },
    { metric: "connections", limit: "direct_connections", label: "Connections" },
  ],
  vector: [{ metric: "storage_bytes", limit: "storage_mb", label: "Storage", bytes: true, mb: true }],
  queue: [{ metric: "storage_bytes", limit: "storage_mb", label: "Storage", bytes: true, mb: true }],
  auth: [
    { metric: "users", limit: "max_users", label: "Users" },
    { metric: "storage_bytes", limit: "storage_mb", label: "Storage", bytes: true, mb: true },
  ],
  cache: [
    { metric: "memory_bytes", limit: "memory_mb", label: "Memory", bytes: true, mb: true },
    { metric: "connections", limit: "connections", label: "Clients" },
  ],
  storage: [{ metric: "storage_bytes", limit: "storage_mb", label: "Storage", bytes: true, mb: true }],
};

function Overview({ r }: { r: Resource }) {
  const ready = r.status === "active" || r.status === "paused" || r.status === "suspended";
  const { data: conn, error } = useApi<Connection>(ready ? `/v1/resources/${r.id}/connection` : null);
  const { data: usage } = useApi<{ usage: Record<string, number>; limits: Record<string, number> }>(ready ? `/v1/resources/${r.id}/usage` : null, { poll: 30_000 });

  return (
    <div className="grid gap-6 lg:grid-cols-5">
      <div className="space-y-6 lg:col-span-3">
        <Card>
          <CardHeader title="Use it from your app" description="Through the project API with the SDK, or natively." />
          <div className="space-y-4 p-5">
            <CopyField label="Service API URL" value={r.apiUrl} />
            <CodeBlock code={snippet(r)} />
          </div>
        </Card>
        {conn?.native && (
          <Card>
            <CardHeader title="Native connection" description={r.kind === "cache" ? "Any Redis client. Keys must start with the prefix." : "Any Postgres driver, ORM or tool."} />
            <div className="space-y-4 p-5">
              <CopyField label="Connection string" value={String(conn.native.url)} secret />
              {conn.native.pooledUrl && conn.native.pooledUrl !== conn.native.url && (
                <CopyField label="Pooled (transaction mode)" value={String(conn.native.pooledUrl)} secret />
              )}
              {conn.native.keyPrefix && <CopyField label="Key prefix" value={String(conn.native.keyPrefix)} />}
              <div className="grid gap-3 sm:grid-cols-2">
                <CopyField label="Host" value={String(conn.native.host)} />
                <CopyField label="Port" value={String(conn.native.port)} />
                <CopyField label="User" value={String(conn.native.user)} />
                <CopyField label="Password" value={String(conn.native.password)} secret />
              </div>
            </div>
          </Card>
        )}
        {conn?.jwtSecret && (
          <Card>
            <CardHeader title="Verify user tokens on your server" description="Access tokens are HS256 JWTs signed with this secret." />
            <div className="p-5">
              <CopyField label="JWT secret" value={conn.jwtSecret} secret />
            </div>
          </Card>
        )}
        <ErrorText error={error} />
      </div>
      <div className="space-y-6 lg:col-span-2">
        <Card>
          <CardHeader title="Usage" description="Sampled every minute." />
          <div className="space-y-4 p-5">
            {(USAGE_ROWS[r.kind] ?? []).map((row) => {
              const used = usage?.usage[row.metric] ?? 0;
              const lim = r.limits[row.limit] ?? -1;
              return (
                <Meter
                  key={row.metric}
                  label={row.label}
                  used={used}
                  limit={lim < 0 ? -1 : row.mb ? lim * 1024 * 1024 : lim}
                  format={row.bytes ? formatBytes : (n) => n.toLocaleString()}
                />
              );
            })}
            {r.limits.requests_per_month !== undefined && (
              <Meter label="API requests this month" used={usage?.usage.requests_month ?? 0} limit={r.limits.requests_per_month} format={(n) => n.toLocaleString()} />
            )}
            {r.writeLocked && <ErrorText error="Over the plan limit: writes are blocked until usage drops or the plan changes." />}
          </div>
        </Card>
        <Card>
          <CardHeader title="Details" />
          <dl className="grid grid-cols-2 gap-y-3 p-5 text-sm">
            <dt className="text-muted-foreground">Kind</dt>
            <dd>{KIND_LABEL[r.kind]}</dd>
            <dt className="text-muted-foreground">Plan</dt>
            <dd>{r.plan}</dd>
            <dt className="text-muted-foreground">Created</dt>
            <dd>{timeAgo(r.createdAt)}</dd>
            <dt className="text-muted-foreground">Last active</dt>
            <dd>{timeAgo(r.lastActiveAt)}</dd>
          </dl>
        </Card>
      </div>
    </div>
  );
}

function Backups({ r }: { r: Resource }) {
  const { data, reload, error } = useApi<{ backups: Array<{ id: string; status: string; sizeBytes: number | null; createdAt: string; expiresAt: string | null; error: string | null }> }>(
    `/v1/resources/${r.id}/backups`,
    { poll: 5000 },
  );
  const [busy, setBusy] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);
  return (
    <Card>
      <CardHeader
        title="Backups"
        description={`Nightly logical dumps, encrypted, kept ${r.limits.backup_days ?? 3} days on this plan.`}
        action={
          <Button
            loading={busy}
            onClick={async () => {
              setBusy(true);
              setActionError(null);
              try {
                await api("POST", `/v1/resources/${r.id}/backups`);
                reload();
              } catch (e) {
                setActionError(e);
              } finally {
                setBusy(false);
              }
            }}
          >
            Back up now
          </Button>
        }
      />
      <div className="p-2">
        <ErrorText error={error ?? actionError} />
        <Table head={["Taken", "Status", "Size", "Expires", ""]}>
          {data?.backups.map((b) => (
            <tr key={b.id}>
              <Td>{new Date(b.createdAt).toLocaleString()}</Td>
              <Td>
                <Badge>{b.status}</Badge>
                {b.error && <div className="mt-1 text-xs text-destructive">{b.error}</div>}
              </Td>
              <Td>{formatBytes(b.sizeBytes)}</Td>
              <Td>{b.expiresAt ? new Date(b.expiresAt).toLocaleDateString() : "-"}</Td>
              <Td className="text-right">
                {b.status === "done" && (
                  <Button size="sm" variant="secondary" onClick={() => setRestoring(b.id)}>
                    Restore
                  </Button>
                )}
              </Td>
            </tr>
          ))}
        </Table>
        {data?.backups.length === 0 && <p className="p-6 text-center text-sm text-muted-foreground">No backups yet.</p>}
      </div>
      <ConfirmModal
        open={!!restoring}
        onClose={() => setRestoring(null)}
        title="Restore backup"
        body="This replaces the current data in this service with the backup. Changes made since the backup are lost."
        confirmWord={r.name}
        actionLabel="Restore"
        onConfirm={async () => {
          await api("POST", `/v1/resources/${r.id}/backups/${restoring}/restore`);
        }}
      />
    </Card>
  );
}

function Settings({ r, reload }: { r: Resource; reload: () => void }) {
  const router = useRouter();
  const { data: plans } = useApi<{ plans: Array<{ id: string; name: string }> }>(`/v1/plans?kind=${r.kind}`);
  const [name, setName] = useState(r.name);
  const [plan, setPlan] = useState(r.plan);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [confirm, setConfirm] = useState<"delete" | "rotate" | null>(null);

  const run = async (key: string, fn: () => Promise<unknown>) => {
    setBusy(key);
    setError(null);
    try {
      await fn();
      reload();
    } catch (e) {
      setError(e);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-6">
      <ErrorText error={error} />
      <Card>
        <CardHeader title="General" />
        <div className="grid gap-4 p-5 sm:grid-cols-2">
          <Field label="Name" hint="Changing it changes the service's API URL.">
            <div className="flex gap-2">
              <Input value={name} onChange={(e) => setName(e.target.value)} />
              <Button variant="secondary" loading={busy === "name"} disabled={name === r.name} onClick={() => run("name", () => api("PATCH", `/v1/resources/${r.id}`, { name }))}>
                Save
              </Button>
            </div>
          </Field>
          <Field label="Plan" hint="Moving down is refused while usage is above the smaller plan.">
            <div className="flex gap-2">
              <Select value={plan} onChange={(e) => setPlan(e.target.value)}>
                {plans?.plans.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.id})
                  </option>
                ))}
              </Select>
              <Button variant="secondary" loading={busy === "plan"} disabled={plan === r.plan} onClick={() => run("plan", () => api("PATCH", `/v1/resources/${r.id}`, { plan }))}>
                Change
              </Button>
            </div>
          </Field>
        </div>
      </Card>
      <Card>
        <CardHeader title="Lifecycle" />
        <div className="divide-y divide-border">
          <div className="flex flex-wrap items-center justify-between gap-4 p-5">
            <div>
              <div className="font-medium">{r.status === "paused" ? "Resume" : "Pause"}</div>
              <p className="text-sm text-muted-foreground">Paused services refuse connections and API calls. Data is kept.</p>
            </div>
            {r.status === "paused" ? (
              <Button variant="secondary" loading={busy === "resume"} onClick={() => run("resume", () => api("POST", `/v1/resources/${r.id}/resume`))}>
                Resume
              </Button>
            ) : (
              <Button variant="secondary" loading={busy === "pause"} disabled={r.status !== "active"} onClick={() => run("pause", () => api("POST", `/v1/resources/${r.id}/pause`))}>
                Pause
              </Button>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-4 p-5">
            <div>
              <div className="font-medium">Rotate credentials</div>
              <p className="text-sm text-muted-foreground">Issues new native credentials. The old ones keep working for the grace period set by your admin.</p>
            </div>
            <Button variant="secondary" onClick={() => setConfirm("rotate")}>
              Rotate
            </Button>
          </div>
          {r.status === "failed" && (
            <div className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div>
                <div className="font-medium">Retry provisioning</div>
                <p className="text-sm text-muted-foreground">{r.lastError}</p>
              </div>
              <Button loading={busy === "retry"} onClick={() => run("retry", () => api("POST", `/v1/resources/${r.id}/retry`))}>
                Retry
              </Button>
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-4 p-5">
            <div>
              <div className="font-medium text-destructive">Delete service</div>
              <p className="text-sm text-muted-foreground">Removes the service and all its data.</p>
            </div>
            <Button variant="danger" onClick={() => setConfirm("delete")}>
              Delete
            </Button>
          </div>
        </div>
      </Card>
      <ConfirmModal
        open={confirm === "rotate"}
        onClose={() => setConfirm(null)}
        title="Rotate credentials"
        body="New native credentials are issued now. Update your apps before the grace period ends."
        actionLabel="Rotate"
        onConfirm={async () => {
          await api("POST", `/v1/resources/${r.id}/credentials/rotate`);
          reload();
        }}
      />
      <ConfirmModal
        open={confirm === "delete"}
        onClose={() => setConfirm(null)}
        title={`Delete ${r.name}`}
        body="All data in this service is deleted. This cannot be undone."
        confirmWord={r.name}
        actionLabel="Delete service"
        onConfirm={async () => {
          await api("DELETE", `/v1/resources/${r.id}`);
          router.replace(`/projects/${r.projectId}`);
        }}
      />
    </div>
  );
}

export default function ServicePage() {
  const { id, sid } = useParams<{ id: string; sid: string }>();
  const { data, error, loading, reload } = useApi<{ resource: Resource }>(`/v1/resources/${sid}`);
  const [tab, setTab] = useState<Tab>("overview");
  const r = data?.resource;

  useEffect(() => {
    if (r?.status !== "provisioning") return;
    const t = setInterval(reload, 1500);
    return () => clearInterval(t);
  }, [r?.status, reload]);

  if (loading) return <Spinner />;
  if (error || !r) return <ErrorText error={error ?? "Service not found"} />;
  const Icon = KIND_ICON[r.kind];
  const hasBackups = ["postgres", "vector", "queue", "auth"].includes(r.kind);
  const tabs: Array<{ id: Tab; label: string }> = [
    { id: "overview", label: "Overview" },
    { id: "data", label: r.kind === "auth" ? "Users" : "Data" },
    ...(hasBackups ? [{ id: "backups" as Tab, label: "Backups" }] : []),
    { id: "settings", label: "Settings" },
  ];

  return (
    <>
      <PageHeader
        back={<Link href={`/projects/${id}`}>← Project</Link>}
        title={
          <span className="flex items-center gap-3">
            <Icon className="size-6 text-brand" /> {r.name}
          </span>
        }
        description={
          <span className="flex items-center gap-2">
            {KIND_LABEL[r.kind]} · <StatusDot status={r.status} /> {r.status}
            {r.writeLocked && <Badge tone="paused">read-only</Badge>}
          </span>
        }
      />
      {r.status === "provisioning" ? (
        <Card>
          <Spinner label="Creating the service. This takes a few seconds." />
        </Card>
      ) : r.status === "failed" ? (
        <Settings r={r} reload={reload} />
      ) : (
        <div className="space-y-6">
          <Tabs tabs={tabs} value={tab} onChange={setTab} />
          {r.status !== "active" && tab === "data" ? (
            <ErrorText error={`The service is ${r.status}. Resume it to browse data.`} />
          ) : (
            <>
              {tab === "overview" && <Overview r={r} />}
              {tab === "data" && r.kind === "postgres" && <PostgresBrowser r={r} />}
              {tab === "data" && r.kind === "cache" && <CacheBrowser r={r} />}
              {tab === "data" && r.kind === "storage" && <StorageBrowser r={r} />}
              {tab === "data" && r.kind === "vector" && <VectorBrowser r={r} />}
              {tab === "data" && r.kind === "queue" && <QueueBrowser r={r} />}
              {tab === "data" && r.kind === "auth" && <AuthBrowser r={r} />}
              {tab === "backups" && <Backups r={r} />}
              {tab === "settings" && <Settings r={r} reload={reload} />}
            </>
          )}
        </div>
      )}
    </>
  );
}
