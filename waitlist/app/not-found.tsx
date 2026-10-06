import { HomeLink, PageShell } from "@/components/page-shell";

export default function NotFound() {
  return (
    <PageShell>
      <h1 className="text-5xl font-semibold tracking-[-0.04em]">Page not found.</h1>
      <p className="mt-5 text-lg leading-relaxed text-muted-foreground">That page doesn&apos;t exist.</p>
      <HomeLink className="mt-10" />
    </PageShell>
  );
}
