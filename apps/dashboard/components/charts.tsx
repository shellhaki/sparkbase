"use client";

import { useState } from "react";

export interface Point {
  t: string | number;
  v: number;
}

/** A compact time chart in the brand blue, with a hover readout. */
export function TimeChart({
  points,
  kind = "area",
  height = 120,
  format = (n: number) => n.toLocaleString(),
  label,
}: {
  points: Point[];
  kind?: "area" | "bars";
  height?: number;
  format?: (n: number) => string;
  label: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const w = 600;
  const h = height;
  const max = Math.max(1, ...points.map((p) => p.v));
  const step = points.length > 1 ? w / (points.length - 1) : w;
  const y = (v: number) => h - 6 - (v / max) * (h - 16);
  const last = points.at(-1)?.v ?? 0;
  const shown = hover !== null ? points[hover] : points.at(-1);

  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between gap-2">
        <span className="font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">{label}</span>
        <span className="tabular font-mono text-sm">
          {shown ? format(shown.v) : format(last)}
          {hover !== null && shown && (
            <span className="ml-2 text-xs text-muted-foreground">{new Date(shown.t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
          )}
        </span>
      </div>
      <svg
        viewBox={`0 0 ${w} ${h}`}
        preserveAspectRatio="none"
        className="w-full"
        style={{ height }}
        onMouseLeave={() => setHover(null)}
        onMouseMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect();
          const i = Math.round(((e.clientX - r.left) / r.width) * (points.length - 1));
          setHover(Math.max(0, Math.min(points.length - 1, i)));
        }}
        role="img"
        aria-label={`${label}: latest ${format(last)}`}
      >
        {[0.25, 0.5, 0.75].map((g) => (
          <line key={g} x1="0" x2={w} y1={h * g} y2={h * g} stroke="var(--border)" strokeDasharray="3 5" vectorEffect="non-scaling-stroke" />
        ))}
        {kind === "bars"
          ? points.map((p, i) => (
              <rect
                key={i}
                x={i * (w / points.length) + 1}
                y={y(p.v)}
                width={Math.max(1, w / points.length - 2)}
                height={h - 6 - y(p.v)}
                rx="1.5"
                fill="var(--brand)"
                opacity={hover === null || hover === i ? 0.9 : 0.35}
              />
            ))
          : points.length > 1 && (
              <>
                <path d={`M0 ${h} ` + points.map((p, i) => `L${i * step} ${y(p.v)}`).join(" ") + ` L${w} ${h} Z`} fill="var(--brand)" opacity="0.12" />
                <path d={points.map((p, i) => `${i ? "L" : "M"}${i * step} ${y(p.v)}`).join(" ")} fill="none" stroke="var(--brand)" strokeWidth="1.8" vectorEffect="non-scaling-stroke" />
              </>
            )}
        {hover !== null && kind === "area" && points[hover] && (
          <line x1={hover * step} x2={hover * step} y1="0" y2={h} stroke="var(--muted-foreground)" strokeDasharray="2 3" vectorEffect="non-scaling-stroke" />
        )}
      </svg>
    </div>
  );
}
