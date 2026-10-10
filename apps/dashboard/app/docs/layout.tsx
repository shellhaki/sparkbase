import type { Metadata } from "next";
import Image from "next/image";
import Link from "next/link";
import { DocsSearch, DocsSidebar } from "@/components/docs-client";
import { loadNav, searchIndex } from "@/lib/docs";

export const metadata: Metadata = {
  title: { default: "Docs", template: "%s · Sparkbase docs" },
  description: "Everything you need to run Sparkbase and build on it.",
};

export default function DocsLayout({ children }: { children: React.ReactNode }) {
  const nav = loadNav();
  const index = searchIndex();
  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-30 border-b border-border bg-background/80 backdrop-blur-xl">
        <div className="mx-auto flex h-16 max-w-[1400px] items-center gap-4 px-4 md:px-6">
          <Link href="/docs/introduction" className="flex shrink-0 items-center gap-2.5">
            <Image src="/mark.png" alt="" width={22} height={22} />
            <span className="font-display text-[15px] font-semibold tracking-tight">Sparkbase</span>
            <span className="rounded-md border border-border px-1.5 py-0.5 font-mono text-[10px] tracking-wide text-muted-foreground uppercase">Docs</span>
          </Link>
          <DocsSearch index={index} />
          <nav className="ml-auto flex items-center gap-1 text-sm">
            <a
              href="https://github.com/shellhaki/sparkbase"
              target="_blank"
              rel="noreferrer"
              className="hidden rounded-lg px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground sm:block"
            >
              GitHub
            </a>
            <Link href="/" className="rounded-lg bg-brand px-3 py-1.5 font-medium text-on-brand transition-colors hover:bg-brand-hover">
              Dashboard
            </Link>
          </nav>
        </div>
      </header>
      <div className="mx-auto flex max-w-[1400px] gap-8 px-4 md:px-6">
        <DocsSidebar nav={nav} />
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  );
}
