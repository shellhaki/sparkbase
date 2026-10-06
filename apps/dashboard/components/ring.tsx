/** A circular gauge: how much of a limit is used. */
export function Ring({
  used,
  limit,
  label,
  format = (n: number) => n.toLocaleString(),
  color = "var(--brand)",
}: {
  used: number;
  limit: number;
  label: string;
  format?: (n: number) => string;
  color?: string;
}) {
  const unlimited = limit < 0;
  const pct = unlimited || limit === 0 ? 0 : Math.min(1, used / limit);
  const r = 34;
  const c = 2 * Math.PI * r;
  const tone = pct >= 1 ? "var(--destructive)" : pct >= 0.8 ? "var(--warning)" : color;
  return (
    <div className="flex items-center gap-4">
      <svg viewBox="0 0 80 80" className="size-20 shrink-0 -rotate-90" role="img" aria-label={`${label}: ${Math.round(pct * 100)}% used`}>
        <circle cx="40" cy="40" r={r} fill="none" stroke="var(--muted)" strokeWidth="7" />
        <circle
          cx="40"
          cy="40"
          r={r}
          fill="none"
          stroke={tone}
          strokeWidth="7"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - Math.max(pct, 0.005))}
          style={{ transition: "stroke-dashoffset 700ms var(--ease-out)" }}
        />
        <text x="40" y="44" textAnchor="middle" className="fill-foreground font-mono text-[13px]" transform="rotate(90 40 40)">
          {unlimited ? "∞" : `${Math.round(pct * 100)}%`}
        </text>
      </svg>
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        <div className="tabular mt-0.5 font-mono text-xs text-muted-foreground">
          {format(used)} / {unlimited ? "unlimited" : format(limit)}
        </div>
      </div>
    </div>
  );
}
