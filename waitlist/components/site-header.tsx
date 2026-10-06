import Link from "next/link";
import { Button } from "@/components/ui/button";
import { GITHUB_URL } from "@/lib/site";
import { LogoMark, Wordmark } from "./brand";

const links = [
  { href: "/#how-it-works", label: "How it works" },
  { href: "/#resources", label: "Resources" },
  { href: "/#pricing", label: "Pricing" },
  { href: "/#open-source", label: "Open source" },
];

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur">
      <nav
        aria-label="Main"
        className="mx-auto flex h-16 max-w-6xl items-center gap-8 px-5 sm:px-8"
      >
        <Link href="/" className="flex items-center gap-2.5 rounded-md text-lg">
          <LogoMark size={24} />
          <Wordmark />
        </Link>

        <ul className="hidden items-center gap-7 text-[15px] text-muted-foreground md:flex">
          {links.map((link) => (
            <li key={link.href}>
              <Link href={link.href} className="transition-colors hover:text-foreground">
                {link.label}
              </Link>
            </li>
          ))}
          <li>
            <a href={GITHUB_URL} className="transition-colors hover:text-foreground" rel="noopener">
              GitHub
            </a>
          </li>
        </ul>

        {/* The hero form listens for clicks on "#join" links and selects the waitlist. */}
        <Button asChild size="pill-sm" className="ml-auto">
          <Link href="/#join">Join the waitlist</Link>
        </Button>
      </nav>
    </header>
  );
}
