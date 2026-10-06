"use client";

import { Check, CircleAlert } from "lucide-react";
import { useEffect, useState } from "react";

type Toast = { id: number; message: string; tone: "ok" | "error" };

let listeners: Array<(t: Toast[]) => void> = [];
let toasts: Toast[] = [];
let seq = 0;

function emit() {
  for (const l of listeners) l(toasts);
}

/** Shows a short message at the bottom of the screen. Call from anywhere. */
export function toast(message: string, tone: Toast["tone"] = "ok") {
  const t = { id: ++seq, message, tone };
  toasts = [...toasts.slice(-2), t];
  emit();
  setTimeout(() => {
    toasts = toasts.filter((x) => x.id !== t.id);
    emit();
  }, 2400);
}

export function Toaster() {
  const [items, setItems] = useState<Toast[]>([]);
  useEffect(() => {
    listeners.push(setItems);
    return () => {
      listeners = listeners.filter((l) => l !== setItems);
    };
  }, []);
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-5 z-[60] flex flex-col items-center gap-2" role="status" aria-live="polite">
      {items.map((t) => (
        <div key={t.id} className="toast flex items-center gap-2 rounded-full border border-border bg-card/95 px-4 py-2 text-sm shadow-xl backdrop-blur">
          {t.tone === "ok" ? <Check className="size-4 text-success" /> : <CircleAlert className="size-4 text-destructive" />}
          {t.message}
        </div>
      ))}
    </div>
  );
}
