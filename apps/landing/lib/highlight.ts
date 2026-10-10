import "server-only";

import { createHighlighter, type Highlighter } from "shiki";
import { sparkbaseDark } from "./code-theme";

let instance: Promise<Highlighter> | null = null;

/** Highlighted HTML, rendered at build time. */
export async function highlight(code: string, lang: "ts" | "bash" | "sql" | "json"): Promise<string> {
  instance ??= createHighlighter({ themes: [sparkbaseDark], langs: ["ts", "bash", "sql", "json"] });
  return (await instance).codeToHtml(code, { lang, theme: "sparkbase-dark" });
}
