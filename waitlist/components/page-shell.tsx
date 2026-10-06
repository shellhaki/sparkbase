import Link from "next/link";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";

/** Narrow centered column used by the small text pages. */
export function PageShell({ children }: { children: ReactNode }) {
  return <div className="mx-auto max-w-2xl px-5 py-24 sm:px-8 md:py-32">{children}</div>;
}

export function HomeLink({ className = "" }: { className?: string }) {
  return (
    <Button asChild size="pill" className={className}>
      <Link href="/">Back to the homepage</Link>
    </Button>
  );
}
