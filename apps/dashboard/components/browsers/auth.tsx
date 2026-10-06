"use client";

import { Ban, Plus, Trash2, Undo2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Badge, Button, Card, CardHeader, ErrorText, Field, Input, Modal, Table, Td, Textarea } from "../ui";

interface AuthUser {
  id: string;
  email: string;
  data: Record<string, unknown>;
  lastSignInAt: string | null;
  bannedAt: string | null;
  createdAt: string;
}

interface AuthSettings {
  allowSignups: boolean;
  siteUrl: string;
  redirectUrls: string[];
  accessTokenTtl: number;
  refreshTokenTtl: number;
}

function SettingsCard({ base }: { base: string }) {
  const { data, reload } = useApi<{ settings: AuthSettings }>(`${base}/admin/settings`);
  const [form, setForm] = useState<{ allowSignups: boolean; siteUrl: string; redirectUrls: string; accessTokenTtl: string }>({
    allowSignups: true,
    siteUrl: "",
    redirectUrls: "",
    accessTokenTtl: "3600",
  });
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!data) return;
    const s = data.settings;
    setForm({ allowSignups: s.allowSignups, siteUrl: s.siteUrl, redirectUrls: s.redirectUrls.join("\n"), accessTokenTtl: String(s.accessTokenTtl) });
  }, [data]);

  return (
    <Card>
      <CardHeader title="Settings" description="Sign-in links and recovery emails only go to the site URL or an allowlisted redirect." />
      <form
        className="space-y-4 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          setSaved(false);
          try {
            await api("PATCH", `${base}/admin/settings`, {
              allowSignups: form.allowSignups,
              siteUrl: form.siteUrl,
              redirectUrls: form.redirectUrls.split("\n").map((s) => s.trim()).filter(Boolean),
              accessTokenTtl: Number(form.accessTokenTtl),
            });
            setSaved(true);
            reload();
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={form.allowSignups} onChange={(e) => setForm({ ...form, allowSignups: e.target.checked })} className="size-4 accent-[var(--brand)]" />
          Allow new users to sign up
        </label>
        <Field label="Site URL" hint="Your app's URL; the default target for sign-in and recovery links.">
          <Input type="url" value={form.siteUrl} onChange={(e) => setForm({ ...form, siteUrl: e.target.value })} placeholder="https://myapp.com" />
        </Field>
        <Field label="Redirect allowlist" hint="One URL per line. Links may go to these URLs or paths under them.">
          <Textarea rows={3} value={form.redirectUrls} onChange={(e) => setForm({ ...form, redirectUrls: e.target.value })} />
        </Field>
        <Field label="Access token lifetime (seconds)">
          <Input type="number" min={60} max={86400} value={form.accessTokenTtl} onChange={(e) => setForm({ ...form, accessTokenTtl: e.target.value })} />
        </Field>
        <ErrorText error={error} />
        <div className="flex items-center justify-end gap-3">
          {saved && <span className="text-sm text-success">Saved</span>}
          <Button type="submit" loading={busy}>
            Save settings
          </Button>
        </div>
      </form>
    </Card>
  );
}

export function AuthBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const [search, setSearch] = useState("");
  const { data, reload, error } = useApi<{ users: AuthUser[]; total: number }>(`${base}/admin/users?limit=100&email=${encodeURIComponent(search)}`);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ email: "", password: "" });
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
    <div className="grid gap-6 lg:grid-cols-3">
      <Card className="lg:col-span-2">
        <CardHeader
          title={`Users${data ? ` (${data.total.toLocaleString()})` : ""}`}
          description="People who signed up to your app."
          action={
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus className="size-3.5" /> Add user
            </Button>
          }
        />
        <div className="p-3">
          <Input placeholder="Search by email" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        <div className="px-2 pb-2">
          <ErrorText error={error ?? actionError} />
          <Table head={["Email", "Last sign-in", "Created", ""]}>
            {data?.users.map((u) => (
              <tr key={u.id}>
                <Td>
                  {u.email} {u.bannedAt && <Badge tone="suspended">banned</Badge>}
                </Td>
                <Td>{timeAgo(u.lastSignInAt)}</Td>
                <Td>{timeAgo(u.createdAt)}</Td>
                <Td className="text-right whitespace-nowrap">
                  <Button size="sm" variant="ghost" onClick={() => act(() => api("PATCH", `${base}/admin/users/${u.id}`, { banned: !u.bannedAt }))}>
                    {u.bannedAt ? <Undo2 className="size-3.5" /> : <Ban className="size-3.5" />} {u.bannedAt ? "Unban" : "Ban"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Delete ${u.email}`}
                    onClick={() => {
                      if (confirm(`Delete ${u.email}?`)) act(() => api("DELETE", `${base}/admin/users/${u.id}`));
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </Td>
              </tr>
            ))}
          </Table>
          {data?.users.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No users yet.</p>}
        </div>
      </Card>
      <SettingsCard base={base} />
      <Modal open={creating} onClose={() => setCreating(false)} title="Add user">
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            act(async () => {
              await api("POST", `${base}/admin/users`, { email: form.email, password: form.password || undefined });
              setCreating(false);
              setForm({ email: "", password: "" });
            });
          }}
        >
          <Field label="Email">
            <Input type="email" required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} autoFocus />
          </Field>
          <Field label="Password" hint="Optional. Without one, the user signs in with a link or code.">
            <Input type="password" minLength={8} value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Add user</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
