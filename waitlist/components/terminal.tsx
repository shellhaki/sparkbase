"use client";

import { useEffect, useState } from "react";

type Line = { kind: "command" | "output"; text: string };

const LINES: Line[] = [
  { kind: "command", text: "sparkbase resources add postgres" },
  { kind: "command", text: "sparkbase env pull" },
  { kind: "output", text: "DATABASE_URL=postgres://sb_x1y2:••••@a1b2c3.db.example.com:6432/main" },
];

const TYPE_MS = 32;
const PAUSE_MS = 550;
const TOTAL_CHARS = LINES.reduce((sum, line) => sum + line.text.length, 0);

/** Each line cut to what is visible once `shown` characters have been "typed". */
function visibleText(shown: number) {
  let remaining = shown;
  return LINES.map((line) => {
    const count = Math.max(0, Math.min(line.text.length, remaining));
    remaining -= line.text.length;
    return { ...line, text: line.text.slice(0, count) };
  });
}

export function Terminal() {
  // Server and no-JS render the final state; the client restarts the animation if motion is allowed.
  const [shown, setShown] = useState(TOTAL_CHARS);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    let cancelled = false;
    let timer: number;
    let count = 0;
    let lineIndex = 0;
    let lineEnd = LINES[0].text.length;

    function tick() {
      if (cancelled) return;
      const line = LINES[lineIndex];
      // Output lines appear at once, commands are typed.
      count = line.kind === "output" ? lineEnd : count + 1;
      setShown(count);
      if (count >= TOTAL_CHARS) return;
      if (count >= lineEnd) {
        lineIndex += 1;
        lineEnd += LINES[lineIndex].text.length;
        timer = window.setTimeout(tick, PAUSE_MS);
      } else {
        timer = window.setTimeout(tick, TYPE_MS);
      }
    }

    timer = window.setTimeout(() => {
      setShown(0);
      timer = window.setTimeout(tick, PAUSE_MS);
    }, 0);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, []);

  const lines = visibleText(shown);
  const done = shown >= TOTAL_CHARS;
  const activeIndex = done ? -1 : lines.findIndex((l, i) => l.text.length < LINES[i].text.length);

  return (
    <figure className="w-full overflow-hidden rounded-3xl border border-term-line bg-term-bg text-left shadow-[0_0_0_1px_rgba(255,255,255,0.02)]">
      <div className="flex items-center gap-2 border-b border-term-line px-5 py-4" aria-hidden="true">
        <span className="size-3 rounded-full bg-[#2a2a31]" />
        <span className="size-3 rounded-full bg-[#2a2a31]" />
        <span className="size-3 rounded-full bg-[#2a2a31]" />
        <span className="ml-3 font-mono text-xs text-term-muted">~/my-app</span>
      </div>

      {/* Screen readers get the full transcript once; the animated copy is hidden from them. */}
      <pre className="sr-only">
        {LINES.map((l) => (l.kind === "command" ? `$ ${l.text}` : l.text)).join("\n")}
      </pre>
      <pre
        aria-hidden="true"
        className="overflow-x-auto px-5 py-5 font-mono text-[13px] leading-7 text-term-fg sm:px-6 sm:text-sm"
      >
        {lines.map((line, i) => {
          const visible = i <= activeIndex || done || line.text.length > 0;
          if (!visible) return <div key={i}>&nbsp;</div>;
          return (
            <div key={i} className="whitespace-pre">
              {line.kind === "command" ? (
                <>
                  <span className="select-none text-term-muted">$ </span>
                  {line.text}
                </>
              ) : (
                <span className="text-term-muted">{line.text}</span>
              )}
              {(i === activeIndex || (done && i === LINES.length - 1)) && (
                <span className="caret ml-0.5 inline-block h-[1.1em] w-[0.55em] translate-y-[0.2em] bg-brand" />
              )}
            </div>
          );
        })}
      </pre>
      <figcaption className="border-t border-term-line px-5 py-4 text-sm text-term-muted sm:px-6">
        Add a database, pull the connection string, start building.
      </figcaption>
    </figure>
  );
}
