import type { ThemeRegistration } from "shiki";

/** Syntax colors in the dashboard palette: blues and greys, nothing loud. */
function theme(name: string, type: "dark" | "light", c: Record<string, string>): ThemeRegistration {
  return {
    name,
    type,
    colors: { "editor.background": c.bg, "editor.foreground": c.fg },
    tokenColors: [
      {
        scope: ["comment", "punctuation.definition.comment"],
        settings: { foreground: c.comment, fontStyle: "italic" },
      },
      {
        scope: ["keyword", "storage", "storage.type", "storage.modifier", "keyword.operator.new", "keyword.control"],
        settings: { foreground: c.keyword },
      },
      {
        scope: ["string", "string.quoted", "string.template", "punctuation.definition.string"],
        settings: { foreground: c.string },
      },
      {
        scope: ["constant.numeric", "constant.language", "support.constant", "constant.other"],
        settings: { foreground: c.number },
      },
      {
        scope: ["entity.name.function", "support.function", "meta.function-call entity.name.function"],
        settings: { foreground: c.fn },
      },
      {
        scope: ["variable.other.property", "meta.object-literal.key", "support.type.property-name", "variable.other.object.property"],
        settings: { foreground: c.prop },
      },
      {
        scope: ["entity.name.type", "support.type", "support.class", "entity.name.class"],
        settings: { foreground: c.fn },
      },
      {
        scope: ["variable.parameter", "variable.other.readwrite", "variable.other.constant", "variable"],
        settings: { foreground: c.fg },
      },
      {
        scope: ["keyword.operator", "punctuation", "meta.brace"],
        settings: { foreground: c.punct },
      },
      {
        scope: ["entity.name.tag", "keyword.other.DML", "keyword.other.DDL", "keyword.other.sql"],
        settings: { foreground: c.keyword },
      },
      {
        scope: ["variable.other.normal.shell", "variable.other.special.shell", "punctuation.definition.variable.shell"],
        settings: { foreground: c.number },
      },
      {
        scope: ["entity.name.command.shell", "support.function.builtin.shell"],
        settings: { foreground: c.fn },
      },
      {
        scope: ["constant.other.option", "constant.other.option.dash.shell"],
        settings: { foreground: c.prop },
      },
    ],
  };
}

export const sparkbaseDark = theme("sparkbase-dark", "dark", {
  bg: "#0f0f12",
  fg: "#e4e6ee",
  comment: "#6b6f7b",
  keyword: "#5b8cff",
  string: "#9ec1ff",
  number: "#7dd3fc",
  fn: "#ffffff",
  prop: "#c3cbe0",
  punct: "#8a90a0",
});

export const sparkbaseLight = theme("sparkbase-light", "light", {
  bg: "#f3f3f6",
  fg: "#16181f",
  comment: "#8a8f9c",
  keyword: "#1f47e6",
  string: "#2a5bd7",
  number: "#0369a1",
  fn: "#0b0b0d",
  prop: "#3b4256",
  punct: "#6b7080",
});
