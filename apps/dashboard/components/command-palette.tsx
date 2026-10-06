"use client";

import { CornerDownLeft, FolderKanban, KeyRound, Plus, Search, Shield } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { KIND_LABEL, api } from "@/lib/api";
import { ServiceLogo } from "./logos/service-logo";
import { cn } from "./ui";

interface Item {
  id: string;
  label: string;
  hint?: string;
  icon: React.ReactNode;
  href: string;
}

interface ProjectLite {
  id: string;
  name: string;
  ref: string;
}

/** ⌘K: jump to any project, service or page. Opens and closes instantly. */
export function CommandPalette({ isAdmin }: { isAdmin: boolean }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const [items, setItems] = useState<Item[]>([]);
  const input = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    const base: Item[] = [
      { id: "projects", label: "Projects", icon: <FolderKanban className="size-4" />, href: "/" },
      { id: "new", label: "New project", icon: <Plus className="size-4" />, href: "/?new=1" },
      { id: "account", label: "Account and API keys", icon: <KeyRound className="size-4" />, href: "/account" },
      ...(isAdmin ? [{ id: "admin", label: "Admin", icon: <Shield className="size-4" />, href: "/admin" }] : []),
    ];
    try {
      const { projects } = await api<{ projects: ProjectLite[] }>("GET", "/v1/projects");
      const details = await Promise.all(
        projects.slice(0, 25).map((p) =>
          api<{ resources: Array<{ id: string; name: string; kind: string }> }>("GET", `/v1/projects/${p.id}/resources`)
            .then((d) => ({ p, rs: d.resources }))
            .catch(() => ({ p, rs: [] })),
        ),
      );
      for (const { p, rs } of details) {
        base.push({ id: p.id, label: p.name, hint: p.ref, icon: <FolderKanban className="size-4" />, href: `/projects/${p.id}` });
        for (const r of rs) {
          base.push({
            id: r.id,
            label: r.name,
            hint: `${KIND_LABEL[r.kind]} · ${p.name}`,
            icon: <ServiceLogo kind={r.kind} size="sm" className="!size-6 !p-1" />,
            href: `/projects/${p.id}/services/${r.id}`,
          });
        }
      }
    } catch {
      // Navigation entries still work without the project list.
    }
    setItems(base);
  }, [isAdmin]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
      if (e.key === "Escape") setOpen(false);
    };
    const onOpen = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener("open-command-palette", onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("open-command-palette", onOpen);
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    setQ("");
    setActive(0);
    load();
    setTimeout(() => input.current?.focus(), 0);
  }, [open, load]);

  const results = useMemo(() => {
    const s = q.trim().toLowerCase();
    const list = s ? items.filter((i) => (i.label + " " + (i.hint ?? "")).toLowerCase().includes(s)) : items;
    return list.slice(0, 40);
  }, [items, q]);

  if (!open) return null;

  const go = (item: Item | undefined) => {
    if (!item) return;
    setOpen(false);
    router.push(item.href);
  };

  return (
    <div className="fixed inset-0 z-[70] flex items-start justify-center bg-black/50 p-4 pt-[14vh]" onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)}>
      <div role="dialog" aria-modal="true" aria-label="Command palette" className="w-full max-w-xl overflow-hidden rounded-2xl border border-border bg-card shadow-2xl">
        <div className="flex items-center gap-3 border-b border-border px-4">
          <Search className="size-4 text-muted-foreground" />
          <input
            ref={input}
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setActive(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setActive((a) => Math.min(a + 1, results.length - 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActive((a) => Math.max(a - 1, 0));
              } else if (e.key === "Enter") {
                go(results[active]);
              }
            }}
            placeholder="Jump to a project, service or page"
            className="h-14 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            aria-label="Search"
          />
          <kbd className="rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">esc</kbd>
        </div>
        <ul className="max-h-80 overflow-y-auto p-2" role="listbox">
          {results.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">Nothing matches.</li>}
          {results.map((item, i) => (
            <li
              key={item.id + i}
              role="option"
              aria-selected={i === active}
              onMouseMove={() => setActive(i)}
              onClick={() => go(item)}
              className={cn("flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm", i === active ? "bg-accent text-foreground" : "text-muted-foreground")}
            >
              <span className="flex size-6 items-center justify-center">{item.icon}</span>
              <span className="flex-1 truncate text-foreground">{item.label}</span>
              {item.hint && <span className="truncate font-mono text-xs">{item.hint}</span>}
              {i === active && <CornerDownLeft className="size-3.5" />}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/** Opens the palette from a button. */
export function openCommandPalette() {
  window.dispatchEvent(new Event("open-command-palette"));
}
