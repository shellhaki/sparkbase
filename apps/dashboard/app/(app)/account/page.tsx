"use client";

import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { PageHeader, useSession } from "@/components/shell";
import { Button, Card, CardHeader, CopyField, ErrorText, Field, Input, Modal, Select, Table, Td } from "@/components/ui";
import { api, timeAgo, useApi } from "@/lib/api";

interface Key {
  id: string;
  name: string;
  prefix: string;
  projectId: string | null;
  lastUsedAt: string | null;
  expiresAt: string | null;
  createdAt: string;
}

function Profile() {
  const { user, refresh } = useSession();
  const [name, setName] = useState(user.name ?? "");
  const [pw, setPw] = useState({ current: "", next: "" });
  const [msg, setMsg] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [changingPw, setChangingPw] = useState(false);
  const dirty = name !== (user.name ?? "") || changingPw;

  return (
    <Card>
      <CardHeader title="Profile" description={user.email} />
      <form
        className="space-y-4 p-5"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          setMsg(null);
          try {
            await api("PATCH", "/v1/me", { name, ...(pw.next ? { currentPassword: pw.current, newPassword: pw.next } : {}) });
            setPw({ current: "", next: "" });
            setChangingPw(false);
            setMsg("Saved");
            refresh();
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
        </Field>
        {changingPw && (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Current password">
              <Input type="password" autoComplete="current-password" required autoFocus value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} />
            </Field>
            <Field label="New password" hint="At least 8 characters.">
              <Input type="password" autoComplete="new-password" required minLength={8} value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} />
            </Field>
          </div>
        )}
        <ErrorText error={error} />
        <div className="flex items-center justify-between gap-3">
          {changingPw ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setChangingPw(false);
                setPw({ current: "", next: "" });
              }}
            >
              Keep current password
            </Button>
          ) : (
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => {
                setChangingPw(true);
                setMsg(null);
              }}
            >
              Change password
            </Button>
          )}
          <div className="flex items-center gap-3">
            {msg && !dirty && <span className="text-sm text-success">{msg}</span>}
            {dirty && (
              <Button type="submit" loading={busy}>
                Save
              </Button>
            )}
          </div>
        </div>
      </form>
    </Card>
  );
}

function ApiKeys() {
  const { data, reload, error } = useApi<{ keys: Key[] }>("/v1/api-keys");
  const { data: projects } = useApi<{ projects: Array<{ id: string; name: string }> }>("/v1/projects");
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ name: "", projectId: "", expiresInDays: "" });
  const [secret, setSecret] = useState<string | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);
  const close = () => {
    setCreating(false);
    setSecret(null);
  };

  return (
    <Card>
      <CardHeader
        title="Account API keys"
        description="For the CLI, CI and scripts that manage projects. Apps use project keys instead."
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" /> New key
          </Button>
        }
      />
      <div className="p-2">
        <ErrorText error={error ?? actionError} />
        <Table head={["Name", "Key", "Scope", "Last used", "Expires", ""]}>
          {data?.keys.map((k) => (
            <tr key={k.id}>
              <Td>{k.name}</Td>
              <Td mono>{k.prefix}…</Td>
              <Td>{k.projectId ? (projects?.projects.find((p) => p.id === k.projectId)?.name ?? "one project") : "All projects"}</Td>
              <Td>{timeAgo(k.lastUsedAt)}</Td>
              <Td>{k.expiresAt ? new Date(k.expiresAt).toLocaleDateString() : "never"}</Td>
              <Td className="text-right">
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Revoke ${k.name}`}
                  onClick={async () => {
                    if (!confirm(`Revoke ${k.name}? Anything using it stops working.`)) return;
                    try {
                      await api("DELETE", `/v1/api-keys/${k.id}`);
                      reload();
                    } catch (e) {
                      setActionError(e);
                    }
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
        {data?.keys.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No keys yet.</p>}
      </div>
      <Modal open={creating} onClose={close} title={secret ? "Copy your key" : "New API key"}>
        {secret ? (
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">This is the only time the key is shown. Store it somewhere safe.</p>
            <CopyField value={secret} />
            <div className="flex justify-end">
              <Button onClick={close}>Done</Button>
            </div>
          </div>
        ) : (
          <form
            className="space-y-4"
            onSubmit={async (e) => {
              e.preventDefault();
              setActionError(null);
              try {
                const out = await api<{ secret: string }>("POST", "/v1/api-keys", {
                  name: form.name,
                  projectId: form.projectId || undefined,
                  expiresInDays: form.expiresInDays ? Number(form.expiresInDays) : undefined,
                });
                setSecret(out.secret);
                setForm({ name: "", projectId: "", expiresInDays: "" });
                reload();
              } catch (err) {
                setActionError(err);
              }
            }}
          >
            <Field label="Name">
              <Input required maxLength={60} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="CI deploys" autoFocus />
            </Field>
            <Field label="Scope">
              <Select value={form.projectId} onChange={(e) => setForm({ ...form, projectId: e.target.value })}>
                <option value="">All my projects</option>
                {projects?.projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    Only {p.name}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Expires after (days, optional)">
              <Input type="number" min={1} max={3650} value={form.expiresInDays} onChange={(e) => setForm({ ...form, expiresInDays: e.target.value })} />
            </Field>
            <ErrorText error={actionError} />
            <div className="flex justify-end">
              <Button type="submit">Create key</Button>
            </div>
          </form>
        )}
      </Modal>
    </Card>
  );
}

export default function AccountPage() {
  return (
    <>
      <PageHeader title="Account" />
      <div className="space-y-6">
        <Profile />
        <ApiKeys />
      </div>
    </>
  );
}
