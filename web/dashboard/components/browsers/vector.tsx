"use client";

import { Plus, Search, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Field, Input, Modal, Select, Table, Td, Textarea } from "../ui";

interface Collection {
  name: string;
  dimension: number;
  metric: string;
  createdAt: string;
  count?: number;
}

export function VectorBrowser({ r }: { r: Resource }) {
  const base = `${servicePath(r)}/collections`;
  const { data, reload, error } = useApi<{ collections: Collection[] }>(base);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState({ name: "", dimension: "1536", metric: "cosine" });
  const [querying, setQuerying] = useState<Collection | null>(null);
  const [vector, setVector] = useState("");
  const [matches, setMatches] = useState<Array<{ id: string; score: number; metadata: unknown }> | null>(null);
  const [actionError, setActionError] = useState<unknown>(null);

  return (
    <Card>
      <CardHeader
        title="Collections"
        description="Each collection holds vectors of one dimension with JSON metadata."
        action={
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="size-3.5" /> New collection
          </Button>
        }
      />
      <div className="p-2">
        <ErrorText error={error} />
        <Table head={["Name", "Dimension", "Metric", "Items", "Created", ""]}>
          {data?.collections.map((c) => (
            <tr key={c.name}>
              <Td mono>{c.name}</Td>
              <Td>{c.dimension}</Td>
              <Td>{c.metric}</Td>
              <Td>{c.count?.toLocaleString() ?? "-"}</Td>
              <Td>{timeAgo(c.createdAt)}</Td>
              <Td className="text-right whitespace-nowrap">
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setQuerying(c);
                    setMatches(null);
                    setVector("");
                    setActionError(null);
                  }}
                >
                  <Search className="size-3.5" /> Query
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Drop ${c.name}`}
                  onClick={async () => {
                    if (!confirm(`Drop collection ${c.name} and all its vectors?`)) return;
                    await api("DELETE", `${base}/${c.name}`);
                    reload();
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
        {data?.collections.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No collections yet.</p>}
      </div>

      <Modal open={creating} onClose={() => setCreating(false)} title="New collection">
        <form
          className="space-y-4"
          onSubmit={async (e) => {
            e.preventDefault();
            setActionError(null);
            try {
              await api("POST", base, { name: form.name, dimension: Number(form.dimension), metric: form.metric });
              setCreating(false);
              reload();
            } catch (err) {
              setActionError(err);
            }
          }}
        >
          <Field label="Name" hint="Lowercase letters, digits and underscores.">
            <Input required pattern="[a-z][a-z0-9_]*" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoFocus />
          </Field>
          <Field label="Dimension" hint={`Must match your embedding model, e.g. 1536. This plan allows up to ${r.limits.max_dimension ?? 2048}.`}>
            <Input type="number" min={1} required value={form.dimension} onChange={(e) => setForm({ ...form, dimension: e.target.value })} />
          </Field>
          <Field label="Distance metric">
            <Select value={form.metric} onChange={(e) => setForm({ ...form, metric: e.target.value })}>
              <option value="cosine">Cosine</option>
              <option value="l2">Euclidean (L2)</option>
              <option value="inner_product">Inner product</option>
            </Select>
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button type="submit">Create</Button>
          </div>
        </form>
      </Modal>

      <Modal open={!!querying} onClose={() => setQuerying(null)} title={`Query ${querying?.name ?? ""}`} wide>
        <div className="space-y-4">
          <Field label={`Vector (${querying?.dimension} numbers, JSON array)`}>
            <Textarea rows={4} value={vector} onChange={(e) => setVector(e.target.value)} placeholder="[0.12, -0.08, ...]" />
          </Field>
          <ErrorText error={actionError} />
          <div className="flex justify-end">
            <Button
              onClick={async () => {
                setActionError(null);
                try {
                  const v = JSON.parse(vector);
                  const out = await api<{ matches: Array<{ id: string; score: number; metadata: unknown }> }>("POST", `${base}/${querying!.name}/query`, { vector: v, topK: 10 });
                  setMatches(out.matches);
                } catch (err) {
                  setActionError(err);
                }
              }}
            >
              Search
            </Button>
          </div>
          {matches && (
            <Table head={["ID", "Score", "Metadata"]}>
              {matches.map((m) => (
                <tr key={m.id}>
                  <Td mono>{m.id}</Td>
                  <Td>{m.score.toFixed(4)}</Td>
                  <Td mono className="max-w-xs truncate">
                    {JSON.stringify(m.metadata)}
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </div>
      </Modal>
    </Card>
  );
}
