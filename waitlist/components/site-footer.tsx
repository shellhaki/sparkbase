import Link from "next/link";
import { GITHUB_URL, X_URL } from "@/lib/site";
import { Wordmark } from "./brand";

export function SiteFooter() {
  return (
    <footer className="border-t border-border">
      <div className="mx-auto flex max-w-6xl flex-col gap-10 px-5 py-12 sm:px-8 md:flex-row md:items-start md:justify-between">
        <div className="space-y-2">
          <Wordmark className="text-lg" />
          <p className="text-sm text-muted-foreground">The data layer for your backend.</p>
        </div>

        <nav aria-label="Footer">
          <ul className="flex gap-6 text-sm text-muted-foreground">
            <li>
              <a href={GITHUB_URL} rel="noopener" className="transition-colors hover:text-brand">
                GitHub
              </a>
            </li>
            <li>
              <a href={X_URL} rel="noopener" className="transition-colors hover:text-brand">
                X
              </a>
            </li>
            <li>
              <Link href="/privacy" className="transition-colors hover:text-brand">
                Privacy
              </Link>
            </li>
          </ul>
        </nav>
      </div>

      <div className="mx-auto max-w-6xl px-5 pb-10 text-xs text-muted-foreground sm:px-8">
        <p>© {new Date().getFullYear()} Sparkbase</p>
      </div>
    </footer>
  );
}
