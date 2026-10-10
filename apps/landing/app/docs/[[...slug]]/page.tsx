import { ArrowLeft, ArrowRight } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { DocsArticle, DocsToc } from "@/components/docs-client";
import { allSlugs, loadNav, loadPage } from "@/lib/docs";

export const dynamicParams = false;

export function generateStaticParams() {
  return [{ slug: [] }, ...allSlugs().map((s) => ({ slug: [s] }))];
}

export async function generateMetadata({ params }: { params: Promise<{ slug?: string[] }> }): Promise<Metadata> {
  const { slug } = await params;
  if (!slug?.length) return {};
  const page = await loadPage(slug[0]);
  return { title: page.title, description: page.description };
}

export default async function DocPage({ params }: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await params;
  if (!slug?.length) redirect("/docs/introduction");
  const slugs = allSlugs();
  if (slug.length > 1 || !slugs.includes(slug[0])) notFound();
  const page = await loadPage(slug[0]);
  const nav = loadNav();
  const flat = nav.flatMap((g) => g.pages.map((p) => ({ ...p, group: g.group })));
  const i = flat.findIndex((p) => p.slug === page.slug);
  const prev = flat[i - 1];
  const next = flat[i + 1];
  const group = flat[i]?.group;

  return (
    <div className="flex gap-10">
      <article className="min-w-0 flex-1 py-10 lg:py-14">
        <div className="mb-2 font-mono text-[11px] tracking-[0.14em] text-brand uppercase">{group}</div>
        <h1 className="font-display text-4xl font-semibold tracking-tight">{page.title}</h1>
        {page.description && <p className="mt-3 text-lg text-muted-foreground">{page.description}</p>}
        <DocsArticle html={page.html} />
        <div className="mt-16 grid gap-3 border-t border-border pt-8 sm:grid-cols-2">
          {prev ? (
            <Link href={`/docs/${prev.slug}`} className="group rounded-xl border border-border p-4 transition-colors hover:border-brand/50">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <ArrowLeft className="size-3.5" /> Previous
              </div>
              <div className="mt-1 font-medium group-hover:text-brand">{prev.title}</div>
            </Link>
          ) : (
            <span />
          )}
          {next && (
            <Link href={`/docs/${next.slug}`} className="group rounded-xl border border-border p-4 text-right transition-colors hover:border-brand/50">
              <div className="flex items-center justify-end gap-1.5 text-xs text-muted-foreground">
                Next <ArrowRight className="size-3.5" />
              </div>
              <div className="mt-1 font-medium group-hover:text-brand">{next.title}</div>
            </Link>
          )}
        </div>
        <a
          href={`https://github.com/shellhaki/sparkbase/edit/main/docs/${page.slug}.md`}
          target="_blank"
          rel="noreferrer"
          className="mt-6 inline-block text-sm text-muted-foreground hover:text-foreground"
        >
          Edit this page on GitHub
        </a>
      </article>
      <DocsToc toc={page.toc} />
    </div>
  );
}
