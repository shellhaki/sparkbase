"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

/** A one-line shell command with a copy button. */
export function CopyCommand({ command, className = "" }: { command: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className={`flex items-center gap-3 rounded-xl border border-border bg-code py-2 pr-2 pl-4 font-mono text-[13px] ${className}`}>
      <span className="select-none text-brand">$</span>
      <code className="min-w-0 flex-1 truncate text-foreground/90">{command}</code>
      <button
        type="button"
        onClick={async () => {
          await navigator.clipboard.writeText(command);
          setCopied(true);
          setTimeout(() => setCopied(false), 1400);
        }}
        className="flex size-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        aria-label="Copy command"
      >
        {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
}
