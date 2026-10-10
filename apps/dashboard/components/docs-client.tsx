"use client";

import { CornerDownLeft, Menu, Search, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "./ui";

interface NavGroup {
  group: string;
  pages: Array<{ slug: string; title: string }>;
}

export function DocsSidebar({ nav }: { nav: NavGroup[] }) {
  const pathname = usePathname();
  const [open, setOpen] = useState(false);
  useEffect(() => setOpen(false), [pathname]);
  const list = (
    <nav className="space-y-7 py-8">
      {nav.map((g) => (
        <div key={g.group}>
          <div className="mb-2 px-3 font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">{g.group}</div>
          <div className="space-y-0.5">
            {g.pages.map((p) => {
              const active = pathname === `/docs/${p.slug}`;
              return (
                <Link
                  key={p.slug}
                  href={`/docs/${p.slug}`}
                  className={cn(
                    "relative block rounded-lg px-3 py-1.5 text-sm transition-colors",
                    active ? "bg-brand/10 font-medium text-brand" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                  )}
                >
                  {p.title}
                </Link>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
  return (
    <>
      <aside className="sticky top-16 hidden h-[calc(100dvh-4rem)] w-60 shrink-0 overflow-y-auto pr-2 md:block">{list}</aside>
      <button
        onClick={() => setOpen(true)}
        className="fixed right-4 bottom-4 z-40 flex size-11 items-center justify-center rounded-full bg-brand text-on-brand shadow-xl md:hidden"
        aria-label="Open docs menu"
      >
        <Menu className="size-5" />
      </button>
      {open && (
        <div className="fixed inset-0 z-50 bg-black/60 md:hidden" onClick={() => setOpen(false)}>
          <div className="animate-fade-in h-full w-72 overflow-y-auto border-r border-border bg-background px-3" onClick={(e) => e.stopPropagation()}>
            <button onClick={() => setOpen(false)} className="mt-4 ml-auto block rounded p-2" aria-label="Close">
              <X className="size-4" />
            </button>
            {list}
          </div>
        </div>
      )}
    </>
  );
}

/** Renders a page's HTML and wires copy buttons on code blocks. */
export function DocsArticle({ html }: { html: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onClick = async (e: MouseEvent) => {
      const btn = (e.target as HTMLElement).closest("button.copy") as HTMLButtonElement | null;
      if (!btn) return;
      await navigator.clipboard.writeText(btn.dataset.code ?? "");
      btn.textContent = "Copied";
      setTimeout(() => (btn.textContent = "Copy"), 1400);
    };
    el.addEventListener("click", onClick);
    return () => el.removeEventListener("click", onClick);
  }, [html]);
  return <div ref={ref} className="docs-prose mt-10" dangerouslySetInnerHTML={{ __html: html }} />;
}

/** "On this page", highlighting the heading in view. */
export function DocsToc({ toc }: { toc: Array<{ id: string; text: string; depth: number }> }) {
  const [active, setActive] = useState<string | null>(null);
  useEffect(() => {
    const heads = toc.map((t) => document.getElementById(t.id)).filter(Boolean) as HTMLElement[];
    const obs = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActive(visible[0].target.id);
      },
      { rootMargin: "-80px 0px -65% 0px" },
    );
    heads.forEach((h) => obs.observe(h));
    return () => obs.disconnect();
  }, [toc]);
  if (toc.length === 0) return <div className="hidden w-52 shrink-0 xl:block" />;
  return (
    <aside className="sticky top-16 hidden h-[calc(100dvh-4rem)] w-52 shrink-0 overflow-y-auto py-14 xl:block">
      <div className="mb-3 font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">On this page</div>
      <ul className="space-y-1.5 border-l border-border text-sm">
        {toc.map((t) => (
          <li key={t.id}>
            <a
              href={`#${t.id}`}
              className={cn(
                "-ml-px block border-l py-0.5 transition-colors",
                t.depth === 3 ? "pl-6" : "pl-3",
                active === t.id ? "border-brand text-brand" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.text}
            </a>
          </li>
        ))}
      </ul>
    </aside>
  );
}

interface IndexEntry {
  slug: string;
  title: string;
  section: string;
  headings: string[];
}

function slug(text: string) {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");
}

/** ⌘K search over page titles and headings. Opens instantly, no animation. */
export function DocsSearch({ index }: { index: IndexEntry[] }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
      if (e.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (open) {
      setQ("");
      setActive(0);
      setTimeout(() => input.current?.focus(), 0);
    }
  }, [open]);

  const results = useMemo(() => {
    const s = q.trim().toLowerCase();
    const items = index.flatMap((p) => [
      { href: `/docs/${p.slug}`, title: p.title, hint: p.section },
      ...p.headings.map((h) => ({
        href: `/docs/${p.slug}#${slug(h)}`,
        title: h,
        hint: p.title,
      })),
    ]);
    if (!s) return items.filter((i) => !i.href.includes("#")).slice(0, 12);
    return items.filter((i) => (i.title + " " + i.hint).toLowerCase().includes(s)).slice(0, 20);
  }, [q, index]);

  const go = (href?: string) => {
    if (!href) return;
    setOpen(false);
    router.push(href);
  };

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        className="flex h-9 w-full max-w-xs items-center gap-2 rounded-lg border border-border bg-card/60 px-3 text-sm text-muted-foreground transition-colors hover:border-brand/40 hover:text-foreground"
      >
        <Search className="size-4" />
        <span className="flex-1 text-left">Search docs</span>
        <kbd className="hidden rounded border border-border px-1.5 font-mono text-[10px] sm:inline">⌘K</kbd>
      </button>
      {open &&
        createPortal(
          <div className="fixed inset-0 z-[70] flex items-start justify-center bg-black/50 p-4 pt-[14vh]" onMouseDown={(e) => e.target === e.currentTarget && setOpen(false)}>
            <div role="dialog" aria-modal="true" aria-label="Search docs" className="w-full max-w-xl overflow-hidden rounded-2xl border border-border bg-card shadow-2xl">
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
                    } else if (e.key === "Enter") go(results[active]?.href);
                  }}
                  placeholder="Search the docs"
                  className="h-14 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground focus-visible:outline-none"
                  aria-label="Search"
                />
              </div>
              <ul className="max-h-80 overflow-y-auto p-2">
                {results.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">Nothing matches.</li>}
                {results.map((r, i) => (
                  <li
                    key={r.href}
                    onMouseMove={() => setActive(i)}
                    onClick={() => go(r.href)}
                    className={cn("flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-sm", i === active ? "bg-accent" : "")}
                  >
                    <span className="flex-1 truncate">{r.title}</span>
                    <span className="truncate text-xs text-muted-foreground">{r.hint}</span>
                    {i === active && <CornerDownLeft className="size-3.5 text-muted-foreground" />}
                  </li>
                ))}
              </ul>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}
