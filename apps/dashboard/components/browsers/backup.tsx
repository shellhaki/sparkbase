"use client";

import { Play } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "../toast";
import { api, formatBytes, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Badge, Button, Card, CardHeader, ErrorText, Field, Input, Select, Table, Td } from "../ui";

interface Settings {
  schedule: string;
  retentionDays: number;
  targets: string[] | null;
  destination: { type: string; endpoint?: string; bucket?: string; region?: string; prefix?: string; accessKey?: string };
  hasSecret: boolean;
  nextRunAt: string | null;
}

export function BackupServiceBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const { data, reload } = useApi<{ settings: Settings }>(`${base}/settings`);
  const runs = useApi<{ runs: Array<{ backup: { id: string; status: string; sizeBytes: number | null; createdAt: string; destination: string; error: string | null }; service: string }> }>(`${base}/runs`, { poll: 5000 });
  const { data: project } = useApi<{ resources: Resource[] }>(`/v1/projects/${r.projectId}`);
  const dbs = (project?.resources ?? []).filter((x) => ["postgres", "vector", "queue", "auth"].includes(x.kind));
  const [form, setForm] = useState({ schedule: "0 3 * * *", retentionDays: "7", targets: [] as string[], type: "local", endpoint: "", bucket: "", region: "", prefix: "", accessKey: "", secretKey: "" });
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
      endpoint: s.destination.endpoint ?? "",
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
              const destination =
                form.type === "s3"
                  ? { type: "s3", endpoint: form.endpoint, bucket: form.bucket, region: form.region, prefix: form.prefix, accessKey: form.accessKey, secretKey: form.secretKey || undefined }
                  : { type: "local" };
              await api("PATCH", `${base}/settings`, { schedule: form.schedule, retentionDays: Number(form.retentionDays), targets: form.targets, destination });
              toast("Saved");
              reload();
            } catch (err) {
              setError(err);
            }
          }}
        >
          <Field label="Databases">
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
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Schedule (UTC)">
              <Input value={form.schedule} onChange={(e) => setForm({ ...form, schedule: e.target.value })} className="font-mono" />
            </Field>
            <Field label="Keep (days)">
              <Input type="number" min={1} value={form.retentionDays} onChange={(e) => setForm({ ...form, retentionDays: e.target.value })} />
            </Field>
          </div>
          <Field label="Destination" hint="An off-site bucket survives losing this server.">
            <Select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })}>
              <option value="local">This server</option>
              <option value="s3">S3-compatible bucket</option>
            </Select>
          </Field>
          {form.type === "s3" && (
            <div className="grid grid-cols-2 gap-3">
              <Input placeholder="https://s3.eu-west-1.amazonaws.com" value={form.endpoint} onChange={(e) => setForm({ ...form, endpoint: e.target.value })} className="col-span-2" />
              <Input placeholder="bucket" value={form.bucket} onChange={(e) => setForm({ ...form, bucket: e.target.value })} />
              <Input placeholder="region" value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} />
              <Input placeholder="access key" value={form.accessKey} onChange={(e) => setForm({ ...form, accessKey: e.target.value })} />
              <Input type="password" placeholder={data?.settings.hasSecret ? "secret (unchanged)" : "secret key"} value={form.secretKey} onChange={(e) => setForm({ ...form, secretKey: e.target.value })} />
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
                <Td>{b.destination === "s3" ? "S3" : "server"}</Td>
                <Td>{timeAgo(b.createdAt)}</Td>
              </tr>
            ))}
          </Table>
          {runs.data && runs.data.runs.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No runs yet. Restore a backup from the database&apos;s own Backups tab.</p>}
        </div>
      </Card>
    </div>
  );
}
