"use client";

import { Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { PageHeader, useSession } from "@/components/shell";
import { Badge, Button, Card, CardHeader, CopyField, ErrorText, Field, Input, Modal, Select, SkeletonCards, SkeletonRows, StatusDot, Table, Tabs, Td, Textarea } from "@/components/ui";
import { KIND_LABEL, api, timeAgo, useApi } from "@/lib/api";
import type { Resource } from "@/lib/kinds";
import { ServiceLogo } from "@/components/logos/service-logo";

type Tab = "users" | "services" | "plans" | "hosts" | "settings" | "activity" | "health";

interface User {
  id: string;
  email: string;
  name: string | null;
  role: string;
  lastLoginAt: string | null;
  disabledAt: string | null;
  createdAt: string;
}
interface Invite {
  id: string;
  email: string;
  role: string;
  expiresAt: string;
}

function LinkModal({ link, onClose, title, note }: { link: string | null; onClose: () => void; title: string; note: string }) {
  return (
    <Modal open={!!link} onClose={onClose} title={title}>
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{note}</p>
        {link && <CopyField value={link} />}
        <div className="flex justify-end">
          <Button onClick={onClose}>Done</Button>
        </div>
      </div>
    </Modal>
  );
}

function Users() {
  const { user: me } = useSession();
  const { data, reload, error } = useApi<{ users: User[]; invites: Invite[] }>("/v1/admin/users");
  const [inviting, setInviting] = useState(false);
  const [form, setForm] = useState({ email: "", role: "member" });
  const [link, setLink] = useState<{ url: string; title: string; note: string } | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);

  const act = async (fn: () => Promise<unknown>) => {
    setActionError(null);
    try {
      await fn();
      reload();
    } catch (e) {
      setActionError(e);
    }
  };

  return (
    <div className="space-y-6">
      <ErrorText error={error ?? actionError} />
      <Card>
        <CardHeader
          title="Users"
          action={
            <Button size="sm" onClick={() => setInviting(true)}>
              <Plus className="size-3.5" /> Invite
            </Button>
          }
        />
        <div className="p-2">
          <Table head={["Email", "Role", "Last sign-in", "Status", ""]}>
            {data?.users.map((u) => (
              <tr key={u.id}>
                <Td>
                  <div>{u.email}</div>
                  {u.name && <div className="text-xs text-muted-foreground">{u.name}</div>}
                </Td>
                <Td>
                  <Select
                    className="h-8 w-28"
                    value={u.role}
                    disabled={u.id === me.id}
                    onChange={(e) => act(() => api("PATCH", `/v1/admin/users/${u.id}`, { role: e.target.value }))}
                    aria-label={`Role for ${u.email}`}
                  >
                    <option value="member">member</option>
                    <option value="admin">admin</option>
                  </Select>
                </Td>
                <Td>{timeAgo(u.lastLoginAt)}</Td>
                <Td>{u.disabledAt ? <Badge tone="suspended">disabled</Badge> : <Badge tone="active">active</Badge>}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() =>
                      act(async () => {
                        const out = await api<{ link: string }>("POST", `/v1/admin/users/${u.id}/reset-link`);
                        setLink({ url: out.link, title: "Password reset link", note: `Send this to ${u.email}. It works once and expires in one hour.` });
                      })
                    }
                  >
                    Reset link
                  </Button>
                  {u.id !== me.id && (
                    <Button size="sm" variant="ghost" onClick={() => act(() => api("PATCH", `/v1/admin/users/${u.id}`, { disabled: !u.disabledAt }))}>
                      {u.disabledAt ? "Enable" : "Disable"}
                    </Button>
                  )}
                </Td>
              </tr>
            ))}
          </Table>
        </div>
      </Card>
      {data && data.invites.length > 0 && (
        <Card>
          <CardHeader title="Pending invites" />
          <div className="p-2">
            <Table head={["Email", "Role", "Expires", ""]}>
              {data.invites.map((i) => (
                <tr key={i.id}>
                  <Td>{i.email}</Td>
                  <Td>{i.role}</Td>
                  <Td>{new Date(i.expiresAt).toLocaleDateString()}</Td>
                  <Td className="text-right">
                    <Button size="sm" variant="ghost" onClick={() => act(() => api("DELETE", `/v1/admin/invites/${i.id}`))}>
                      Cancel
                    </Button>
                  </Td>
                </tr>
              ))}
            </Table>
          </div>
        </Card>
      )}
      <Modal open={inviting} onClose={() => setInviting(false)} title="Invite someone">
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            act(async () => {
              const out = await api<{ link: string; emailed: boolean }>("POST", "/v1/admin/invites", form);
              setInviting(false);
              setForm({ email: "", role: "member" });
              setLink({
                url: out.link,
                title: "Invite created",
                note: out.emailed ? `It was emailed to ${form.email}. You can also share this link. It expires in 7 days.` : `Email is not configured on this install, so share this link with ${form.email}. It expires in 7 days.`,
              });
            });
          }}
        >
          <Field label="Email">
            <Input type="email" required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} autoFocus />
          </Field>
          <Field label="Role" hint="Admins manage users, plans, hosts and every project.">
            <Select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
              <option value="member">Member</option>
              <option value="admin">Admin</option>
            </Select>
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Create invite</Button>
          </div>
        </form>
      </Modal>
      <LinkModal link={link?.url ?? null} onClose={() => setLink(null)} title={link?.title ?? ""} note={link?.note ?? ""} />
    </div>
  );
}

function Services() {
  const { data, reload, error } = useApi<{ resources: Resource[] }>("/v1/admin/resources");
  const [actionError, setActionError] = useState<unknown>(null);
  return (
    <Card>
      <CardHeader title="All services" description="Every service on this install." />
      <div className="p-2">
        <ErrorText error={error ?? actionError} />
        <Table head={["Service", "Kind", "Project", "Plan", "Status", ""]}>
          {data?.resources.map((r) => (
            <tr key={r.id}>
              <Td>
                <Link className="flex items-center gap-2 hover:underline" href={`/projects/${r.projectId}/services/${r.id}`}>
                  <ServiceLogo kind={r.kind} size="sm" /> {r.name}
                </Link>
              </Td>
              <Td>{KIND_LABEL[r.kind]}</Td>
              <Td mono>{r.projectRef}</Td>
              <Td>{r.plan}</Td>
              <Td>
                <span className="flex items-center gap-1.5">
                  <StatusDot status={r.status} /> {r.status}
                </span>
              </Td>
              <Td className="text-right">
                {(r.status === "active" || r.status === "paused" || r.status === "suspended") && (
                  <Button
                    size="sm"
                    variant={r.status === "suspended" ? "secondary" : "danger"}
                    onClick={async () => {
                      setActionError(null);
                      try {
                        await api("POST", `/v1/admin/resources/${r.id}/${r.status === "suspended" ? "unsuspend" : "suspend"}`);
                        reload();
                      } catch (e) {
                        setActionError(e);
                      }
                    }}
                  >
                    {r.status === "suspended" ? "Unsuspend" : "Suspend"}
                  </Button>
                )}
              </Td>
            </tr>
          ))}
        </Table>
      </div>
    </Card>
  );
}

interface Plan {
  id: string;
  kind: string;
  name: string;
  limits: Record<string, number>;
  isDefault: boolean;
  active: boolean;
}

function Plans() {
  const { data, reload, error } = useApi<{ plans: Plan[] }>("/v1/admin/plans");
  const [editing, setEditing] = useState<Plan | null>(null);
  const [creating, setCreating] = useState(false);
  const [limits, setLimits] = useState("");
  const [form, setForm] = useState({ id: "", kind: "postgres", name: "" });
  const [actionError, setActionError] = useState<unknown>(null);

  const kinds = Array.from(new Set(data?.plans.map((p) => p.kind) ?? []));
  return (
    <div className="space-y-6">
      <div className="flex justify-end">
        <Button
          size="sm"
          onClick={() => {
            setCreating(true);
            setLimits('{\n  "storage_mb": 2048\n}');
          }}
        >
          <Plus className="size-3.5" /> New plan
        </Button>
      </div>
      <ErrorText error={error} />
      {kinds.map((kind) => (
        <Card key={kind}>
          <CardHeader title={KIND_LABEL[kind] ?? kind} description="Limits presets. -1 means unlimited." />
          <div className="p-2">
            <Table head={["Plan", "Limits", ""]}>
              {data?.plans
                .filter((p) => p.kind === kind)
                .map((p) => (
                  <tr key={p.id} className={p.active ? undefined : "opacity-50"}>
                    <Td>
                      <div className="font-medium">
                        {p.name} {p.isDefault && <Badge tone="active">default</Badge>} {!p.active && <Badge>inactive</Badge>}
                      </div>
                      <div className="font-mono text-xs text-muted-foreground">{p.id}</div>
                    </Td>
                    <Td mono className="text-xs">
                      {Object.entries(p.limits)
                        .map(([k, v]) => `${k}=${v}`)
                        .join("  ")}
                    </Td>
                    <Td className="text-right">
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setEditing(p);
                          setLimits(JSON.stringify(p.limits, null, 2));
                          setActionError(null);
                        }}
                      >
                        Edit
                      </Button>
                    </Td>
                  </tr>
                ))}
            </Table>
          </div>
        </Card>
      ))}
      <Modal open={!!editing} onClose={() => setEditing(null)} title={`Edit ${editing?.name ?? ""}`} wide>
        {editing && (
          <div className="space-y-4">
            <Field label="Limits (JSON)" hint="Changes apply to services on this plan when their limits are next applied or checked.">
              <Textarea rows={10} value={limits} onChange={(e) => setLimits(e.target.value)} />
            </Field>
            <ErrorText error={actionError} />
            <div className="flex flex-wrap justify-end gap-2">
              {!editing.isDefault && (
                <Button
                  variant="secondary"
                  onClick={async () => {
                    try {
                      await api("PATCH", `/v1/admin/plans/${editing.id}`, { isDefault: true });
                      setEditing(null);
                      reload();
                    } catch (e) {
                      setActionError(e);
                    }
                  }}
                >
                  Make default
                </Button>
              )}
              <Button
                variant="secondary"
                onClick={async () => {
                  try {
                    await api("PATCH", `/v1/admin/plans/${editing.id}`, { active: !editing.active });
                    setEditing(null);
                    reload();
                  } catch (e) {
                    setActionError(e);
                  }
                }}
              >
                {editing.active ? "Deactivate" : "Activate"}
              </Button>
              <Button
                onClick={async () => {
                  try {
                    await api("PATCH", `/v1/admin/plans/${editing.id}`, { limits: JSON.parse(limits) });
                    setEditing(null);
                    reload();
                  } catch (e) {
                    setActionError(e);
                  }
                }}
              >
                Save limits
              </Button>
            </div>
          </div>
        )}
      </Modal>
      <Modal open={creating} onClose={() => setCreating(false)} title="New plan" wide>
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setActionError(null);
            try {
              await api("POST", "/v1/admin/plans", { ...form, limits: JSON.parse(limits) });
              setCreating(false);
              reload();
            } catch (err) {
              setActionError(err);
            }
          }}
        >
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Kind">
              <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
                {Object.keys(KIND_LABEL).map((k) => (
                  <option key={k} value={k}>
                    {KIND_LABEL[k]}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Id">
              <Input required pattern="[a-z][a-z0-9-]+" value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder={`${form.kind}-xl`} />
            </Field>
            <Field label="Name">
              <Input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="XL" />
            </Field>
          </div>
          <Field label="Limits (JSON)">
            <Textarea rows={8} value={limits} onChange={(e) => setLimits(e.target.value)} />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Create plan</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}

interface Host {
  id: string;
  kind: string;
  name: string;
  internalAddr: string;
  status: string;
  weight: number;
  resourceCount: number;
  config: Record<string, unknown>;
}

function Hosts() {
  const { data, reload, error } = useApi<{ hosts: Host[] }>("/v1/admin/hosts");
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ kind: "postgres", name: "", internalAddr: "", adminSecret: "", adminUser: "postgres", root: "", weight: "100" });
  const [actionError, setActionError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  return (
    <Card>
      <CardHeader
        title="Data hosts"
        description="Servers that hold services. New services go to the least-loaded active host of their kind."
        action={
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus className="size-3.5" /> Add host
          </Button>
        }
      />
      <div className="p-2">
        <ErrorText error={error ?? actionError} />
        <Table head={["Host", "Kind", "Address", "Services", "Weight", "Status"]}>
          {data?.hosts.map((h) => (
            <tr key={h.id}>
              <Td>
                {h.name} {Boolean(h.config.hasVector) && <Badge>pgvector</Badge>}
              </Td>
              <Td>{h.kind}</Td>
              <Td mono>{h.kind === "storage" ? String(h.config.root ?? "") : h.internalAddr}</Td>
              <Td>{h.resourceCount}</Td>
              <Td>{h.weight}</Td>
              <Td>
                <Select
                  className="h-8 w-32"
                  value={h.status}
                  aria-label={`Status of ${h.name}`}
                  onChange={async (e) => {
                    setActionError(null);
                    try {
                      await api("PATCH", `/v1/admin/hosts/${h.id}`, { status: e.target.value });
                      reload();
                    } catch (err) {
                      setActionError(err);
                    }
                  }}
                >
                  <option value="active">active</option>
                  <option value="draining">draining</option>
                  <option value="offline">offline</option>
                </Select>
              </Td>
            </tr>
          ))}
        </Table>
      </div>
      <Modal open={adding} onClose={() => setAdding(false)} title="Add a data host">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setActionError(null);
            try {
              await api("POST", "/v1/admin/hosts", {
                kind: form.kind,
                name: form.name,
                internalAddr: form.internalAddr,
                adminSecret: form.adminSecret,
                weight: Number(form.weight),
                config: form.kind === "postgres" ? { adminUser: form.adminUser } : form.kind === "storage" ? { backend: "local", root: form.root } : {},
              });
              setAdding(false);
              reload();
            } catch (err) {
              setActionError(err);
            } finally {
              setBusy(false);
            }
          }}
        >
          <Field label="Kind">
            <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value })}>
              <option value="postgres">Postgres (postgres, vector, queue, auth)</option>
              <option value="cache">Redis (cache)</option>
              <option value="storage">Local storage directory</option>
            </Select>
          </Field>
          <Field label="Name">
            <Input required pattern="[a-z][a-z0-9-]+" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="pg-2" />
          </Field>
          {form.kind === "storage" ? (
            <Field label="Root directory" hint="Absolute path on the server running Sparkbase.">
              <Input required value={form.root} onChange={(e) => setForm({ ...form, root: e.target.value })} placeholder="/var/lib/sparkbase/storage" />
            </Field>
          ) : (
            <>
              <Field label="Internal address" hint="host:port reachable from the Sparkbase server.">
                <Input required value={form.internalAddr} onChange={(e) => setForm({ ...form, internalAddr: e.target.value })} placeholder="10.0.0.5:5432" />
              </Field>
              {form.kind === "postgres" && (
                <Field label="Admin user" hint="A superuser on that server.">
                  <Input required value={form.adminUser} onChange={(e) => setForm({ ...form, adminUser: e.target.value })} />
                </Field>
              )}
              <Field label="Admin password">
                <Input type="password" value={form.adminSecret} onChange={(e) => setForm({ ...form, adminSecret: e.target.value })} />
              </Field>
            </>
          )}
          <Field label="Weight" hint="Relative capacity. A host with weight 200 gets twice the services of one with 100.">
            <Input type="number" min={1} value={form.weight} onChange={(e) => setForm({ ...form, weight: e.target.value })} />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit" loading={busy}>
              Add host
            </Button>
          </div>
        </form>
      </Modal>
    </Card>
  );
}

interface Settings {
  autoPauseDays: number;
  resourceCap: number;
  membersChangePlans: boolean;
  rotationGraceSeconds: number;
}

function InstallSettings() {
  const { data, reload } = useApi<{ settings: Settings; smtpConfigured: boolean }>("/v1/admin/settings");
  const [form, setForm] = useState<Settings | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (data) setForm(data.settings);
  }, [data]);
  if (!form) return <SkeletonRows rows={5} />;
  return (
    <Card>
      <CardHeader title="Install settings" description={data?.smtpConfigured ? "Email is configured." : "Email is not configured: invite and reset links are shown to admins instead."} />
      <form
        className="space-y-5 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          setError(null);
          setSaved(false);
          try {
            await api("PATCH", "/v1/admin/settings", form);
            setSaved(true);
            reload();
          } catch (err) {
            setError(err);
          }
        }}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Services per member" hint="-1 for unlimited. Admins have no cap.">
            <Input type="number" min={-1} value={form.resourceCap} onChange={(e) => setForm({ ...form, resourceCap: Number(e.target.value) })} />
          </Field>
          <Field label="Auto-pause idle services after (days)" hint="0 turns it off.">
            <Input type="number" min={0} max={365} value={form.autoPauseDays} onChange={(e) => setForm({ ...form, autoPauseDays: Number(e.target.value) })} />
          </Field>
          <Field label="Credential rotation grace period (seconds)">
            <Input type="number" min={0} value={form.rotationGraceSeconds} onChange={(e) => setForm({ ...form, rotationGraceSeconds: Number(e.target.value) })} />
          </Field>
        </div>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="size-4 accent-[var(--brand)]" checked={form.membersChangePlans} onChange={(e) => setForm({ ...form, membersChangePlans: e.target.checked })} />
          Members may change their services' plans
        </label>
        <ErrorText error={error} />
        <div className="flex items-center justify-end gap-3">
          {saved && <span className="text-sm text-success">Saved</span>}
          <Button type="submit">Save settings</Button>
        </div>
      </form>
    </Card>
  );
}

function Activity() {
  const { data, error } = useApi<{ entries: Array<{ id: number; action: string; actorEmail: string | null; resourceId: string | null; detail: unknown; createdAt: string }> }>(
    "/v1/admin/audit?limit=200",
  );
  return (
    <Card>
      <CardHeader title="Audit log" description="Every create, delete, rotation, plan change, pause, suspension, invite and limit event." />
      <div className="max-h-[640px] overflow-y-auto p-2">
        <ErrorText error={error} />
        <Table head={["When", "Action", "By", "Detail"]}>
          {data?.entries.map((a) => (
            <tr key={a.id}>
              <Td className="whitespace-nowrap">{new Date(a.createdAt).toLocaleString()}</Td>
              <Td mono>{a.action}</Td>
              <Td>{a.actorEmail ?? "system"}</Td>
              <Td mono className="max-w-sm truncate text-xs">
                {a.detail ? JSON.stringify(a.detail) : ""}
              </Td>
            </tr>
          ))}
        </Table>
      </div>
    </Card>
  );
}

function Health() {
  const { data: status } = useApi<{ healthy: boolean; hosts: Array<Host & { reachable: boolean; error?: string }>; jobs: { queued: number; running: number; failed24h: number } }>(
    "/v1/status",
    { poll: 10_000 },
  );
  const { data: jobs } = useApi<{ jobs: Array<{ id: number; type: string; status: string; attempts: number; lastError: string | null; createdAt: string }> }>(
    "/v1/admin/jobs?status=failed&limit=50",
  );
  if (!status) return <SkeletonCards count={3} />;
  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-4">
        {[
          { label: "Overall", value: status.healthy ? "Healthy" : "Degraded", tone: status.healthy ? "text-success" : "text-destructive" },
          { label: "Jobs queued", value: status.jobs.queued },
          { label: "Jobs running", value: status.jobs.running },
          { label: "Failed (24 h)", value: status.jobs.failed24h, tone: status.jobs.failed24h ? "text-destructive" : undefined },
        ].map((s) => (
          <Card key={s.label} className="p-5">
            <div className="text-xs text-muted-foreground">{s.label}</div>
            <div className={`mt-1 font-display text-2xl font-semibold ${s.tone ?? ""}`}>{s.value}</div>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader title="Hosts" />
        <div className="p-2">
          <Table head={["Host", "Kind", "Status", "Reachable"]}>
            {status.hosts.map((h) => (
              <tr key={h.id}>
                <Td>{h.name}</Td>
                <Td>{h.kind}</Td>
                <Td>
                  <Badge>{h.status}</Badge>
                </Td>
                <Td className={h.reachable ? "text-success" : "text-destructive"}>{h.reachable ? "yes" : h.error}</Td>
              </tr>
            ))}
          </Table>
        </div>
      </Card>
      <Card>
        <CardHeader title="Failed jobs" />
        <div className="p-2">
          <Table head={["Job", "Attempts", "Error", "Created"]}>
            {jobs?.jobs.map((j) => (
              <tr key={j.id}>
                <Td mono>{j.type}</Td>
                <Td>{j.attempts}</Td>
                <Td className="max-w-md text-xs text-destructive">{j.lastError}</Td>
                <Td>{timeAgo(j.createdAt)}</Td>
              </tr>
            ))}
          </Table>
          {jobs?.jobs.length === 0 && <p className="p-6 text-center text-sm text-muted-foreground">No failed jobs.</p>}
        </div>
      </Card>
    </div>
  );
}

export default function AdminPage() {
  const { user } = useSession();
  const router = useRouter();
  const [tab, setTab] = useState<Tab>("users");
  useEffect(() => {
    if (user.role !== "admin") router.replace("/");
  }, [user, router]);
  if (user.role !== "admin") return null;
  return (
    <>
      <PageHeader title="Admin" />
      <div className="space-y-6">
        <Tabs
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "users", label: "Users" },
            { id: "services", label: "Services" },
            { id: "plans", label: "Plans" },
            { id: "hosts", label: "Hosts" },
            { id: "settings", label: "Settings" },
            { id: "activity", label: "Audit log" },
            { id: "health", label: "Health" },
          ]}
        />
        {tab === "users" && <Users />}
        {tab === "services" && <Services />}
        {tab === "plans" && <Plans />}
        {tab === "hosts" && <Hosts />}
        {tab === "settings" && <InstallSettings />}
        {tab === "activity" && <Activity />}
        {tab === "health" && <Health />}
      </div>
    </>
  );
}
