import "server-only";

import fs from "node:fs";
import path from "node:path";
import { Marked, type Tokens } from "marked";
import { createHighlighter, type Highlighter } from "shiki";
import { sparkbaseDark } from "./code-theme";

/** Where the Markdown lives: the repo's docs/ folder, or a copy in content/docs. */
function docsDir(): string {
  const candidates = [path.join(process.cwd(), "content/docs"), path.join(process.cwd(), "../../docs")];
  for (const c of candidates) {
    if (fs.existsSync(path.join(c, "nav.json"))) return c;
  }
  throw new Error("docs folder not found");
}

export interface NavGroup {
  group: string;
  pages: Array<{ slug: string; title: string }>;
}

export interface TocItem {
  id: string;
  text: string;
  depth: number;
}

export interface DocPage {
  slug: string;
  title: string;
  description: string;
  html: string;
  toc: TocItem[];
}

function parseFront(raw: string): {
  meta: Record<string, string>;
  body: string;
} {
  const m = /^---\n([\s\S]*?)\n---\n/.exec(raw);
  if (!m) return { meta: {}, body: raw };
  const meta: Record<string, string> = {};
  for (const line of m[1].split("\n")) {
    const i = line.indexOf(":");
    if (i > 0) meta[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  }
  return { meta, body: raw.slice(m[0].length) };
}

function readRaw(slug: string) {
  return parseFront(fs.readFileSync(path.join(docsDir(), `${slug}.md`), "utf8"));
}

export function loadNav(): NavGroup[] {
  const nav = JSON.parse(fs.readFileSync(path.join(docsDir(), "nav.json"), "utf8")) as Array<{ group: string; pages: string[] }>;
  return nav.map((g) => ({
    group: g.group,
    pages: g.pages.map((slug) => ({
      slug,
      title: readRaw(slug).meta.title ?? slug,
    })),
  }));
}

export function allSlugs(): string[] {
  return loadNav().flatMap((g) => g.pages.map((p) => p.slug));
}

export function slugify(text: string): string {
  return text
    .toLowerCase()
    .replace(/<[^>]+>/g, "")
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");
}

let highlighter: Promise<Highlighter> | null = null;
function getHighlighter() {
  highlighter ??= createHighlighter({
    themes: [sparkbaseDark],
    langs: ["ts", "js", "bash", "json", "sql", "go", "yaml", "toml", "http", "text"],
  });
  return highlighter;
}

function escapeHtml(s: string) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export async function loadPage(slug: string): Promise<DocPage> {
  const { meta, body } = readRaw(slug);
  const hl = await getHighlighter();
  const toc: TocItem[] = [];
  const marked = new Marked({
    gfm: true,
    renderer: {
      heading({ tokens, depth }: Tokens.Heading) {
        const text = this.parser.parseInline(tokens);
        const id = slugify(text);
        if (depth === 2 || depth === 3) toc.push({ id, text: text.replace(/<[^>]+>/g, ""), depth });
        return `<h${depth} id="${id}"><a href="#${id}" class="anchor">${text}</a></h${depth}>`;
      },
      code({ text, lang }: Tokens.Code) {
        const language = (lang ?? "text").split(/\s/)[0];
        let html: string;
        try {
          html = hl.codeToHtml(text, {
            lang: hl.getLoadedLanguages().includes(language) ? language : "text",
            theme: "sparkbase-dark",
          });
        } catch {
          html = `<pre><code>${escapeHtml(text)}</code></pre>`;
        }
        return `<div class="code-block" data-lang="${escapeHtml(language)}"><div class="code-head"><span>${escapeHtml(language)}</span><button type="button" class="copy" data-code="${escapeHtml(text)}">Copy</button></div>${html}</div>`;
      },
      blockquote({ tokens }: Tokens.Blockquote) {
        // "> **Label** text" becomes a callout.
        const inner = this.parser.parse(tokens);
        return `<div class="callout">${inner}</div>`;
      },
      link({ href, title, tokens }: Tokens.Link) {
        const text = this.parser.parseInline(tokens);
        let url = href;
        const external = /^[a-z]+:/i.test(href);
        if (!external && !href.startsWith("/") && !href.startsWith("#")) {
          url = href.startsWith("../") ? `https://github.com/shellhaki/sparkbase/blob/main/${href.replace(/^\.\.\//, "")}` : `/docs/${href}`;
        }
        const t = title ? ` title="${escapeHtml(title)}"` : "";
        const ext = /^https?:/.test(url) ? ` target="_blank" rel="noreferrer"` : "";
        return `<a href="${escapeHtml(url)}"${t}${ext}>${text}</a>`;
      },
      table(token: Tokens.Table) {
        const head = token.header.map((c) => `<th>${this.parser.parseInline(c.tokens)}</th>`).join("");
        const rows = token.rows.map((r) => `<tr>${r.map((c) => `<td>${this.parser.parseInline(c.tokens)}</td>`).join("")}</tr>`).join("");
        return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${rows}</tbody></table></div>`;
      },
    },
  });
  const html = await marked.parse(body);
  return {
    slug,
    title: meta.title ?? slug,
    description: meta.description ?? "",
    html,
    toc,
  };
}

/** Titles and headings of every page, for search. */
export function searchIndex(): Array<{
  slug: string;
  title: string;
  section: string;
  headings: string[];
}> {
  return loadNav().flatMap((g) =>
    g.pages.map((p) => {
      const { body } = readRaw(p.slug);
      const headings = [...body.matchAll(/^#{2,3} (.+)$/gm)].map((m) => m[1]);
      return { slug: p.slug, title: p.title, section: g.group, headings };
    }),
  );
}
