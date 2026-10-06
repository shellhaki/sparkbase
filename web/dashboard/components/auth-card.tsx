import Image from "next/image";
import type { ReactNode } from "react";

/** Centered card used by the sign-in, setup, invite and reset pages. */
export function AuthCard({ title, description, children, footer }: { title: string; description?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <main className="flex min-h-dvh flex-col items-center justify-center px-4 py-12">
      <div className="mb-8 flex items-center gap-2">
        <Image src="/mark.png" alt="" width={28} height={28} priority />
        <span className="font-display text-lg font-semibold">Sparkbase</span>
      </div>
      <div className="w-full max-w-sm rounded-xl border border-border bg-card p-6 shadow-sm">
        <h1 className="font-display text-xl font-semibold">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
        <div className="mt-6">{children}</div>
      </div>
      {footer && <div className="mt-6 text-sm text-muted-foreground">{footer}</div>}
    </main>
  );
}
