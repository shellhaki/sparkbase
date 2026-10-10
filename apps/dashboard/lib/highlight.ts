import type { HighlighterCore } from "shiki/core";
import { sparkbaseDark, sparkbaseLight } from "./code-theme";

export type CodeLang = "ts" | "bash" | "sql" | "json";

let instance: Promise<HighlighterCore> | null = null;

/** Loaded on first use, with the JavaScript regex engine so no wasm ships. */
function highlighter() {
  instance ??= (async () => {
    const [{ createHighlighterCore }, { createJavaScriptRegexEngine }] = await Promise.all([import("shiki/core"), import("shiki/engine/javascript")]);
    return createHighlighterCore({
      themes: [sparkbaseDark, sparkbaseLight],
      langs: [import("shiki/langs/typescript.mjs"), import("shiki/langs/bash.mjs"), import("shiki/langs/sql.mjs"), import("shiki/langs/json.mjs")],
      engine: createJavaScriptRegexEngine(),
    });
  })();
  return instance;
}

const LANG: Record<CodeLang, string> = {
  ts: "typescript",
  bash: "bash",
  sql: "sql",
  json: "json",
};

/** HTML for a code snippet; colors switch with the color scheme through CSS variables. */
export async function highlight(code: string, lang: CodeLang): Promise<string> {
  const hl = await highlighter();
  return hl.codeToHtml(code, {
    lang: LANG[lang],
    themes: { dark: sparkbaseDark.name!, light: sparkbaseLight.name! },
    defaultColor: false,
  });
}

/** A best guess when the caller does not say. */
export function guessLang(code: string): CodeLang {
  const first = code.trimStart();
  if (/^(curl|npm|bun|pnpm|yarn|docker|sparkbase|psql|redis-cli|export|cd|git|\$ )/.test(first)) return "bash";
  if (/^(select|insert|update|delete|create|alter|with)\b/i.test(first)) return "sql";
  if (/^[{[]/.test(first)) return "json";
  return "ts";
}
