"use client";

import { Check, Copy, Eye, EyeOff, Loader2, X } from "lucide-react";
import { useEffect, useState, type ButtonHTMLAttributes, type CSSProperties, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";

export function cn(...parts: Array<string | false | null | undefined>) {
  return parts.filter(Boolean).join(" ");
}

type Variant = "primary" | "secondary" | "ghost" | "danger";

export function Button({
  variant = "primary",
  size = "md",
  loading,
  className,
  children,
  disabled,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: "sm" | "md"; loading?: boolean }) {
  const styles: Record<Variant, string> = {
    primary: "bg-brand text-on-brand hover:bg-brand-hover",
    secondary: "border border-border bg-card hover:bg-accent",
    ghost: "hover:bg-accent text-muted-foreground hover:text-foreground",
    danger: "border border-destructive/40 text-destructive hover:bg-destructive/10",
  };
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-[background-color,color,transform] duration-150 active:scale-[0.97] disabled:pointer-events-none disabled:opacity-50",
        size === "sm" ? "h-8 px-3 text-sm" : "h-10 px-4 text-sm",
        styles[variant],
        className,
      )}
      disabled={disabled || loading}
      {...props}
    >
      {loading && <Loader2 className="size-4 animate-spin" aria-hidden />}
      {children}
    </button>
  );
}

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        "h-10 w-full rounded-lg border border-input bg-background px-3 text-sm placeholder:text-muted-foreground focus:border-brand focus:outline-none",
        className,
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        "w-full rounded-lg border border-input bg-background px-3 py-2 font-mono text-sm placeholder:text-muted-foreground focus:border-brand focus:outline-none",
        className,
      )}
      {...props}
    />
  );
}

export function Select({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn("h-10 w-full rounded-lg border border-input bg-background px-3 text-sm focus:border-brand focus:outline-none", className)}
      {...props}
    >
      {children}
    </select>
  );
}

export function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-sm font-medium">{label}</span>
      {children}
      {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
    </label>
  );
}

export function Card({ className, children, style }: { className?: string; children: ReactNode; style?: CSSProperties }) {
  return (
    <div className={cn("rounded-xl border border-border bg-card", className)} style={style}>
      {children}
    </div>
  );
}

export function CardHeader({ title, description, action }: { title: ReactNode; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-4">
      <div className="min-w-0">
        <h2 className="font-display text-base font-semibold">{title}</h2>
        {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
      </div>
      {action}
    </div>
  );
}

const STATUS_STYLES: Record<string, string> = {
  active: "bg-success/15 text-success",
  provisioning: "bg-brand/15 text-brand",
  paused: "bg-warning/15 text-warning",
  suspended: "bg-destructive/15 text-destructive",
  failed: "bg-destructive/15 text-destructive",
  deleting: "bg-muted text-muted-foreground",
  draining: "bg-warning/15 text-warning",
  offline: "bg-destructive/15 text-destructive",
  queued: "bg-muted text-muted-foreground",
  running: "bg-brand/15 text-brand",
  done: "bg-success/15 text-success",
};

export function Badge({ children, tone }: { children: ReactNode; tone?: string }) {
  return (
    <span className={cn("inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium", STATUS_STYLES[tone ?? String(children)] ?? "bg-muted text-muted-foreground")}>
      {children}
    </span>
  );
}

export function StatusDot({ status }: { status: string }) {
  const color =
    status === "active" ? "bg-success" : status === "provisioning" ? "bg-brand animate-pulse" : status === "paused" ? "bg-warning" : "bg-destructive";
  return <span className={cn("inline-block size-2 rounded-full", color)} aria-hidden />;
}

export function useCopy() {
  const [copied, setCopied] = useState<string | null>(null);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(null), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return {
    copied,
    copy: async (value: string, id = value) => {
      await navigator.clipboard.writeText(value);
      setCopied(id);
    },
  };
}

/** A read-only value with copy and optional reveal. */
export function CopyField({ label, value, secret, mono = true }: { label?: string; value: string; secret?: boolean; mono?: boolean }) {
  const [shown, setShown] = useState(!secret);
  const { copied, copy } = useCopy();
  return (
    <div className="space-y-1.5">
      {label && <div className="text-xs font-medium text-muted-foreground">{label}</div>}
      <div className="flex items-center gap-1 rounded-lg border border-input bg-background pl-3">
        <code className={cn("min-w-0 flex-1 truncate py-2 text-sm", mono && "font-mono")}>{shown ? value : "•".repeat(Math.min(value.length, 32))}</code>
        {secret && (
          <button type="button" onClick={() => setShown(!shown)} className="rounded p-2 text-muted-foreground hover:text-foreground" aria-label={shown ? "Hide" : "Reveal"}>
            {shown ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
          </button>
        )}
        <button type="button" onClick={() => copy(value)} className="rounded p-2 text-muted-foreground hover:text-foreground" aria-label="Copy">
          {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
        </button>
      </div>
    </div>
  );
}

export function CodeBlock({ code, className }: { code: string; className?: string }) {
  const { copied, copy } = useCopy();
  return (
    <div className={cn("relative rounded-lg border border-border bg-code", className)}>
      <button type="button" onClick={() => copy(code)} className="absolute top-2 right-2 rounded p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" aria-label="Copy code">
        {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
      </button>
      <pre className="overflow-x-auto p-4 pr-12 font-mono text-[13px] leading-relaxed">{code}</pre>
    </div>
  );
}

export function Modal({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean }) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div
      className="animate-fade-in fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/60 p-4 pt-[10vh] backdrop-blur-sm"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div role="dialog" aria-modal="true" aria-label={title} className={cn("animate-pop-in w-full rounded-xl border border-border bg-card shadow-2xl", wide ? "max-w-2xl" : "max-w-md")}>
        <div className="flex items-center justify-between border-b border-border px-5 py-3">
          <h2 className="font-display font-semibold">{title}</h2>
          <button onClick={onClose} className="rounded p-1 text-muted-foreground hover:text-foreground" aria-label="Close">
            <X className="size-4" />
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <p role="alert" className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {msg}
    </p>
  );
}

/** A shimmering placeholder block. */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("skeleton", className)} aria-hidden />;
}

/** Placeholder grid of cards while a list loads. */
export function SkeletonCards({ count = 3 }: { count?: number }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Loading">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="rounded-xl border border-border bg-card p-5" style={{ animationDelay: `${i * 60}ms` }}>
          <div className="flex items-center gap-3">
            <Skeleton className="size-11 rounded-xl" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-3 w-1/3" />
            </div>
          </div>
          <Skeleton className="mt-6 h-3 w-2/3" />
        </div>
      ))}
    </div>
  );
}

/** Placeholder rows for tables and panels. */
export function SkeletonRows({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3 p-5" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-5" />
      ))}
    </div>
  );
}

export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
      <Loader2 className="size-4 animate-spin" aria-hidden /> {label}
    </div>
  );
}

export function Empty({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-12 text-center">
      <p className="font-display font-semibold">{title}</p>
      {children && <p className="max-w-md text-sm text-muted-foreground">{children}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: Array<{ id: T; label: string }>; value: T; onChange: (t: T) => void }) {
  return (
    <div role="tablist" className="flex gap-1 overflow-x-auto border-b border-border">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={cn(
            "-mb-px shrink-0 border-b-2 px-3 py-2 text-sm font-medium transition-colors",
            value === t.id ? "border-brand text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}

export function Table({ head, children, empty }: { head: ReactNode[]; children: ReactNode; empty?: ReactNode }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-border text-xs text-muted-foreground uppercase">
          <tr>
            {head.map((h, i) => (
              <th key={i} className="px-4 py-2.5 font-medium whitespace-nowrap">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border">{children}</tbody>
      </table>
      {empty}
    </div>
  );
}

export function Td({ children, className, mono }: { children: ReactNode; className?: string; mono?: boolean }) {
  return <td className={cn("px-4 py-2.5 align-top", mono && "font-mono text-[13px]", className)}>{children}</td>;
}

export function Meter({ used, limit, label, format = (n: number) => String(n) }: { used: number; limit: number; label: string; format?: (n: number) => string }) {
  const unlimited = limit < 0;
  const pct = unlimited || limit === 0 ? 0 : Math.min(100, (used / limit) * 100);
  const tone = pct >= 100 ? "bg-destructive" : pct >= 80 ? "bg-warning" : "bg-brand";
  return (
    <div className="space-y-1.5">
      <div className="flex justify-between text-sm">
        <span>{label}</span>
        <span className="text-muted-foreground tabular-nums">
          {format(used)} / {unlimited ? "unlimited" : format(limit)}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
        <div className={cn("h-full rounded-full transition-all", tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

/** Asks for confirmation by typing a word before a destructive action. */
export function ConfirmModal({
  open,
  onClose,
  title,
  body,
  confirmWord,
  actionLabel,
  onConfirm,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  body: ReactNode;
  confirmWord?: string;
  actionLabel: string;
  onConfirm: () => Promise<void>;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (open) {
      setTyped("");
      setError(null);
    }
  }, [open]);
  return (
    <Modal open={open} onClose={onClose} title={title}>
      <div className="space-y-4">
        <div className="text-sm text-muted-foreground">{body}</div>
        {confirmWord && (
          <Field label={`Type ${confirmWord} to confirm`}>
            <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
          </Field>
        )}
        <ErrorText error={error} />
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="danger"
            loading={busy}
            disabled={!!confirmWord && typed !== confirmWord}
            onClick={async () => {
              setBusy(true);
              setError(null);
              try {
                await onConfirm();
                onClose();
              } catch (e) {
                setError(e);
              } finally {
                setBusy(false);
              }
            }}
          >
            {actionLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
