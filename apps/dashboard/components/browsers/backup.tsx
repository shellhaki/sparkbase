"use client";

import { Play } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "../toast";
import { api, formatBytes, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Badge, Button, Card, CardHeader, ErrorText, Field, Input, Table, Td, Group } from "../ui";

interface Settings {
  schedule: string;
  retentionDays: number;
  targets: string[] | null;
  destination: { type: string; accountId?: string; jurisdiction?: string; endpoint?: string; bucket?: string; region?: string; prefix?: string; accessKey?: string };
  hasSecret: boolean;
  nextRunAt: string | null;
}

export function BackupServiceBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data, reload } = useApi<{ settings: Settings }>(`${base}/settings`);
  const runs = useApi<{ runs: Array<{ backup: { id: string; status: string; sizeBytes: number | null; createdAt: string; destination: string; error: string | null }; service: string }> }>(
    `${base}/runs`,
    { poll: 5000 },
  );
  const { data: project } = useApi<{ resources: Resource[] }>(`/v1/projects/${r.projectId}`);
  const dbs = (project?.resources ?? []).filter((x) => ["postgres", "vector", "queue", "auth"].includes(x.kind));
  const [form, setForm] = useState({
    schedule: "0 3 * * *",
    retentionDays: "7",
    targets: [] as string[],
    type: "local",
    accountId: "",
    eu: false,
    endpoint: "",
    bucket: "",
    region: "",
    prefix: "",
    accessKey: "",
    secretKey: "",
  });
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!data) return;
    const s = data.settings;
    setForm((f) => ({
      ...f,
      schedule: s.schedule,
      retentionDays: String(s.retentionDays),
      targets: s.targets ?? [],
      type: s.destination.type,
      accountId: s.destination.accountId ?? "",
      eu: s.destination.jurisdiction === "eu",
      endpoint: s.destination.type === "r2" ? "" : (s.destination.endpoint ?? ""),
      bucket: s.destination.bucket ?? "",
      region: s.destination.region ?? "",
      prefix: s.destination.prefix ?? "",
      accessKey: s.destination.accessKey ?? "",
    }));
  }, [data]);

  return (
    <div className="grid gap-6 lg:grid-cols-5">
      <Card className="lg:col-span-2">
        <CardHeader title="Policy" />
        <form
          className="space-y-4 p-5"
          onSubmit={async (e) => {
            e.preventDefault();
            setError(null);
            try {
              const bucket = { bucket: form.bucket, prefix: form.prefix, accessKey: form.accessKey, secretKey: form.secretKey || undefined };
              const destination =
                form.type === "r2"
                  ? { type: "r2", accountId: form.accountId, jurisdiction: form.eu ? "eu" : "", ...bucket }
                  : form.type === "s3"
                    ? { type: "s3", endpoint: form.endpoint, region: form.region, ...bucket }
                    : { type: "local" };
              await api("PATCH", `${base}/settings`, { schedule: form.schedule, retentionDays: Number(form.retentionDays), targets: form.targets, destination });
              toast(form.type === "local" ? "Saved" : "Saved. The bucket accepted a test write.");
              reload();
            } catch (err) {
              setError(err);
            }
          }}
        >
          <Group label="Databases">
            <div className="flex flex-wrap gap-2">
              {dbs.length === 0 && <span className="text-sm text-muted-foreground">No database services in this project.</span>}
              {dbs.map((d) => {
                const on = form.targets.includes(d.name);
                return (
                  <button
                    type="button"
                    key={d.id}
                    onClick={() => setForm({ ...form, targets: on ? form.targets.filter((t) => t !== d.name) : [...form.targets, d.name] })}
                    className={`rounded-lg border px-3 py-1.5 text-sm transition-colors ${on ? "border-brand bg-brand/10 text-foreground" : "border-border text-muted-foreground hover:text-foreground"}`}
                  >
                    {d.name}
                  </button>
                );
              })}
            </div>
          </Group>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Schedule (UTC)">
              <Input value={form.schedule} onChange={(e) => setForm({ ...form, schedule: e.target.value })} className="font-mono" />
            </Field>
            <Field label="Keep (days)">
              <Input type="number" min={1} value={form.retentionDays} onChange={(e) => setForm({ ...form, retentionDays: e.target.value })} />
            </Field>
          </div>
          <Group label="Destination">
            <div className="inline-flex w-full rounded-lg border border-border p-0.5">
              {[
                ["r2", "Cloudflare R2"],
                ["s3", "Other S3"],
                ["local", "This server"],
              ].map(([v, label]) => (
                <button
                  type="button"
                  key={v}
                  onClick={() => setForm({ ...form, type: v })}
                  className={`flex-1 rounded-md px-2.5 py-1.5 text-sm font-medium transition-colors ${form.type === v ? "bg-accent text-foreground" : "text-muted-foreground hover:text-foreground"}`}
                >
                  {label}
                </button>
              ))}
            </div>
          </Group>
          {form.type === "local" && (
            <p className="rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
              Backups on this server are lost if the server is. Bring your own R2 bucket to keep them somewhere else.
            </p>
          )}
          {form.type === "r2" && (
            <div className="space-y-3">
              <Field label="Account ID" hint="On the R2 overview page in Cloudflare.">
                <Input value={form.accountId} onChange={(e) => setForm({ ...form, accountId: e.target.value })} placeholder="32 hex characters" className="font-mono" />
              </Field>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Bucket">
                  <Input value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} placeholder="my-backups" />
                </Field>
                <Field label="Prefix (optional)">
                  <Input value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} placeholder="sparkbase" />
                </Field>
                <Field label="Access key ID">
                  <Input value={form.accessKey} onChange={(e) => setForm({ ...form, accessKey: e.target.value })} className="font-mono" />
                </Field>
                <Field label="Secret access key">
                  <Input type="password" value={form.secretKey} onChange={(e) => setForm({ ...form, secretKey: e.target.value })} placeholder={data?.settings.hasSecret ? "unchanged" : ""} />
                </Field>
              </div>
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                <input type="checkbox" checked={form.eu} onChange={(e) => setForm({ ...form, eu: e.target.checked })} className="accent-[var(--brand)]" />
                EU jurisdiction bucket
              </label>
              <p className="text-xs text-muted-foreground">Create an R2 API token with Object Read &amp; Write on this bucket. Saving writes and deletes a small test file.</p>
            </div>
          )}
          {form.type === "s3" && (
            <div className="grid grid-cols-2 gap-3">
              <Input placeholder="https://s3.eu-west-1.amazonaws.com" value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} className="col-span-2" />
              <Input placeholder="bucket" value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} />
              <Input placeholder="region" value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} />
              <Input placeholder="access key" value={form.accessKey} onChange={(e) => setForm({ ...form, accessKey: e.target.value })} />
              <Input
                type="password"
                placeholder={data?.settings.hasSecret ? "secret (unchanged)" : "secret key"}
                value={form.secretKey}
                onChange={(e) => setForm({ ...form, secretKey: e.target.value })}
              />
              <Input placeholder="prefix (optional)" value={form.prefix} onChange={(e) => setForm({ ...form, prefix: e.target.value })} className="col-span-2" />
            </div>
          )}
          <ErrorText error={error} />
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">{data?.settings.nextRunAt ? `next run ${new Date(data.settings.nextRunAt).toLocaleString()}` : ""}</span>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Card>
      <Card className="lg:col-span-3">
        <CardHeader
          title="Runs"
          action={
            <Button
              size="sm"
              onClick={async () => {
                const res = await api<{ started: number }>("POST", `${base}/run`);
                toast(`Started ${res.started} backup${res.started === 1 ? "" : "s"}`);
                runs.reload();
              }}
            >
              <Play className="size-3.5" /> Run now
            </Button>
          }
        />
        <div className="p-2">
          <Table head={["Database", "Status", "Size", "Where", "When"]}>
            {runs.data?.runs.map(({ backup: b, service }) => (
              <tr key={b.id}>
                <Td>{service}</Td>
                <Td>
                  <Badge>{b.status}</Badge>
                  {b.error && <div className="mt-1 text-xs text-destructive">{b.error}</div>}
                </Td>
                <Td>{formatBytes(b.sizeBytes)}</Td>
                <Td>{b.destination === "s3" ? (data?.settings.destination.type === "r2" ? "R2" : "bucket") : "server"}</Td>
                <Td>{timeAgo(b.createdAt)}</Td>
              </tr>
            ))}
          </Table>
          {runs.data && runs.data.runs.length === 0 && (
            <p className="p-8 text-center text-sm text-muted-foreground">No runs yet. Restore a backup from the database&apos;s own Backups tab.</p>
          )}
        </div>
      </Card>
    </div>
  );
}
