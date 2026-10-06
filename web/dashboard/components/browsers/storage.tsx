"use client";

import { Download, File, Link2, Trash2, Upload } from "lucide-react";
import { useRef, useState } from "react";
import { api, formatBytes, timeAgo, useApi } from "@/lib/api";
import { servicePath, type Resource } from "@/lib/kinds";
import { Button, Card, CardHeader, ErrorText, Input, Table, Td, useCopy } from "../ui";

interface Obj {
  key: string;
  size: number;
  lastModified: string;
}

const encKey = (k: string) => k.split("/").map(encodeURIComponent).join("/");

export function StorageBrowser({ r }: { r: Resource }) {
  const base = servicePath(r);
  const [prefix, setPrefix] = useState("");
  const { data, reload, error } = useApi<{ objects: Obj[]; truncated: boolean }>(`${base}/objects?prefix=${encodeURIComponent(prefix)}`);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<unknown>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const { copied, copy } = useCopy();

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setBusy(true);
    setActionError(null);
    try {
      for (const f of Array.from(files)) {
        await api("PUT", `${base}/objects/${encKey(prefix + f.name)}`, f, { raw: true });
      }
      reload();
    } catch (e) {
      setActionError(e);
    } finally {
      setBusy(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const signed = async (key: string) => (await api<{ url: string }>("POST", `${base}/sign`, { key, expiresIn: 3600 })).url;

  return (
    <Card>
      <CardHeader
        title="Objects"
        description="Files in this service's private bucket."
        action={
          <>
            <input ref={fileInput} type="file" multiple className="hidden" onChange={(e) => upload(e.target.files)} />
            <Button size="sm" loading={busy} onClick={() => fileInput.current?.click()}>
              <Upload className="size-3.5" /> Upload
            </Button>
          </>
        }
      />
      <div
        className="p-3"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          upload(e.dataTransfer.files);
        }}
      >
        <Input placeholder="Folder prefix, e.g. avatars/ (uploads go here too)" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
        <div className="mt-3">
          <ErrorText error={error ?? actionError} />
        </div>
        <Table head={["Key", "Size", "Modified", ""]}>
          {data?.objects.map((o) => (
            <tr key={o.key}>
              <Td mono>
                <span className="flex items-center gap-2">
                  <File className="size-4 shrink-0 text-muted-foreground" />
                  {o.key}
                </span>
              </Td>
              <Td>{formatBytes(o.size)}</Td>
              <Td>{timeAgo(o.lastModified)}</Td>
              <Td className="text-right whitespace-nowrap">
                <Button size="sm" variant="ghost" onClick={async () => copy(await signed(o.key), o.key)}>
                  <Link2 className="size-3.5" /> {copied === o.key ? "Copied" : "Signed URL"}
                </Button>
                <Button size="sm" variant="ghost" aria-label={`Download ${o.key}`} onClick={async () => window.open(await signed(o.key), "_blank")}>
                  <Download className="size-3.5" />
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Delete ${o.key}`}
                  onClick={async () => {
                    if (!confirm(`Delete ${o.key}?`)) return;
                    await api("DELETE", `${base}/objects/${encKey(o.key)}`);
                    reload();
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </Td>
            </tr>
          ))}
        </Table>
        {data?.objects.length === 0 && <p className="p-8 text-center text-sm text-muted-foreground">No objects. Drop files here or use Upload.</p>}
      </div>
    </Card>
  );
}
