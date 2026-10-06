import Image from "next/image";
import type { ReactNode } from "react";

/** Centered card used by the sign-in, setup, invite and reset pages. */
export function AuthCard({ title, description, children, footer }: { title: string; description?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <main className="relative flex min-h-dvh flex-col items-center justify-center overflow-hidden px-4 py-12">
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 h-[480px] w-[720px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{ background: "radial-gradient(closest-side, color-mix(in srgb, var(--brand) 45%, transparent), transparent)" }}
      />
      <div className="animate-fade-up relative mb-8 flex items-center gap-2">
        <Image src="/mark.png" alt="" width={28} height={28} priority />
        <span className="font-display text-lg font-semibold">Sparkbase</span>
      </div>
      <div className="animate-pop-in relative w-full max-w-sm rounded-xl border border-border bg-card/90 p-6 shadow-xl backdrop-blur">
        <h1 className="font-display text-xl font-semibold">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
        <div className="mt-6">{children}</div>
      </div>
      {footer && <div className="animate-fade-in relative mt-6 text-sm text-muted-foreground">{footer}</div>}
    </main>
  );
}
