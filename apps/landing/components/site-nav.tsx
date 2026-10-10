import Image from "next/image";
import Link from "next/link";
import { GITHUB_URL } from "@/lib/site";

export function GitHubIcon({ className = "size-4" }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="currentColor" aria-hidden>
      <path d="M12 .5C5.65.5.5 5.65.5 12a11.5 11.5 0 0 0 7.86 10.92c.58.1.79-.25.79-.56v-2c-3.2.7-3.87-1.37-3.87-1.37-.53-1.33-1.28-1.69-1.28-1.69-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.19-3.08-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.8 1.19 1.82 1.19 3.08 0 4.41-2.69 5.38-5.25 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z" />
    </svg>
  );
}

export function SiteNav() {
  return (
    <header className="sticky top-0 z-30 border-b border-border/60 bg-background/80 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-6xl items-center gap-6 px-5 sm:px-8">
        <Link href="/" className="flex items-center gap-2.5">
          <Image src="/mark.png" alt="" width={24} height={24} priority />
          <span className="font-display text-[16px] font-semibold tracking-tight">Sparkbase</span>
        </Link>
        <nav className="hidden items-center gap-1 text-sm text-muted-foreground md:flex">
          <a href="#services" className="rounded-lg px-3 py-1.5 transition-colors hover:text-foreground">
            Services
          </a>
          <a href="#scale-to-zero" className="rounded-lg px-3 py-1.5 transition-colors hover:text-foreground">
            Scale to zero
          </a>
          <a href="#install" className="rounded-lg px-3 py-1.5 transition-colors hover:text-foreground">
            Install
          </a>
          <Link href="/docs/introduction" className="rounded-lg px-3 py-1.5 transition-colors hover:text-foreground">
            Docs
          </Link>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-2 rounded-lg border border-border px-3 py-1.5 text-sm font-medium transition-colors hover:border-brand/50"
          >
            <GitHubIcon /> <span className="hidden sm:inline">GitHub</span>
          </a>
          <Link href="/docs/quickstart" className="rounded-lg bg-brand px-3.5 py-1.5 text-sm font-medium text-on-brand transition-colors hover:bg-brand-hover">
            Get started
          </Link>
        </div>
      </div>
    </header>
  );
}
