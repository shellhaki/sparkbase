import { POSTGRES_PATH, REDIS_PATH } from "./paths";

/** Every kind uses the brand blue; the shapes tell services apart. */
export const KIND_COLOR: Record<string, string> = {
  postgres: "var(--brand)",
  cache: "var(--brand)",
  storage: "var(--brand)",
  vector: "var(--brand)",
  queue: "var(--brand)",
  auth: "var(--brand)",
  realtime: "var(--brand)",
  cron: "var(--brand)",
  backup: "var(--brand)",
  monitor: "var(--brand)",
};

function Mark({ kind, color }: { kind: string; color: string }) {
  switch (kind) {
    case "postgres":
      return <path d={POSTGRES_PATH} fill={color} />;
    case "cache":
      return <path d={REDIS_PATH} fill={color} />;
    case "storage":
      // A bucket with its lid: objects stored in a private bucket.
      return (
        <g fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round">
          <ellipse cx="12" cy="5.5" rx="8" ry="2.6" fill={color} fillOpacity=".25" />
          <path d="M4 5.5 5.9 19.2c.15 1.1 2.9 2 6.1 2s5.95-.9 6.1-2L20 5.5" />
          <path d="M5.1 10.5c.9.9 3.6 1.6 6.9 1.6s6-.7 6.9-1.6" strokeOpacity=".6" />
          <path d="M5.8 15.2c.8.8 3.3 1.4 6.2 1.4s5.4-.6 6.2-1.4" strokeOpacity=".4" />
        </g>
      );
    case "vector":
      // Points in embedding space, the nearest ones linked to a query.
      return (
        <g fill={color}>
          <g stroke={color} strokeWidth="1.2" strokeOpacity=".55">
            <line x1="12" y1="12" x2="5" y2="6" />
            <line x1="12" y1="12" x2="19" y2="7.5" />
            <line x1="12" y1="12" x2="16.5" y2="18.5" />
          </g>
          <circle cx="12" cy="12" r="3" />
          <circle cx="5" cy="6" r="2" fillOpacity=".85" />
          <circle cx="19" cy="7.5" r="1.8" fillOpacity=".85" />
          <circle cx="16.5" cy="18.5" r="1.9" fillOpacity=".85" />
          <circle cx="5.5" cy="17.5" r="1.3" fillOpacity=".35" />
          <circle cx="20" cy="15" r="1" fillOpacity=".35" />
          <circle cx="10" cy="3.2" r="1" fillOpacity=".35" />
        </g>
      );
    case "queue":
      // Messages lined up, the next one leaving.
      return (
        <g fill={color}>
          <rect x="2.5" y="8.5" width="4.6" height="7" rx="1.4" fillOpacity=".35" />
          <rect x="8" y="8.5" width="4.6" height="7" rx="1.4" fillOpacity=".6" />
          <rect x="13.5" y="8.5" width="4.6" height="7" rx="1.4" />
          <path d="M19.4 9.2 22 12l-2.6 2.8" fill="none" stroke={color} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </g>
      );
    case "auth":
      // A shield with a keyhole: sign-in for your app's users.
      return (
        <g>
          <path d="M12 2.3 19.5 5v6.1c0 4.9-3.2 8.8-7.5 10.6C7.7 19.9 4.5 16 4.5 11.1V5L12 2.3Z" fill={color} fillOpacity=".22" stroke={color} strokeWidth="1.6" strokeLinejoin="round" />
          <circle cx="12" cy="10.3" r="2.2" fill={color} />
          <path d="M11 11.8h2l.5 4.2h-3l.5-4.2Z" fill={color} />
        </g>
      );
    case "realtime":
      // Signal radiating from a point.
      return (
        <g fill="none" stroke={color} strokeWidth="1.6" strokeLinecap="round">
          <circle cx="12" cy="12" r="2.2" fill={color} />
          <path d="M8.2 8.2a5.4 5.4 0 0 0 0 7.6M15.8 8.2a5.4 5.4 0 0 1 0 7.6" />
          <path d="M5.3 5.3a9.5 9.5 0 0 0 0 13.4M18.7 5.3a9.5 9.5 0 0 1 0 13.4" strokeOpacity=".5" />
        </g>
      );
    case "cron":
      // A clock face with ticks.
      return (
        <g fill="none" stroke={color} strokeWidth="1.6" strokeLinecap="round">
          <circle cx="12" cy="12" r="8.5" />
          <path d="M12 7v5l3.2 2" />
          <path d="M12 3.5v1.2M20.5 12h-1.2M12 20.5v-1.2M3.5 12h1.2" strokeOpacity=".5" />
        </g>
      );
    case "backup":
      // An archive box with a returning arrow.
      return (
        <g fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round">
          <rect x="3.5" y="4" width="17" height="4.5" rx="1.2" />
          <path d="M5 8.5v9.8c0 .7.6 1.2 1.2 1.2h11.6c.7 0 1.2-.5 1.2-1.2V8.5" />
          <path d="M12 11.5v5M9.6 14.2 12 16.6l2.4-2.4" />
        </g>
      );
    case "monitor":
      // A heartbeat line.
      return (
        <g fill="none" stroke={color} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
          <path d="M2.5 12.5h4l2-5 3.5 10 2.5-7 1.5 2h5.5" />
        </g>
      );
  }
  return <circle cx="12" cy="12" r="6" fill={color} />;
}

const SIZES = { sm: "size-8 rounded-lg p-1.5", md: "size-11 rounded-xl p-2.5", lg: "size-14 rounded-2xl p-3" };

/** A tinted tile with the service's logo. */
export function ServiceLogo({ kind, size = "md", className = "" }: { kind: string; size?: keyof typeof SIZES; className?: string }) {
  const color = "currentColor";
  return (
    <span className={`inline-flex shrink-0 items-center justify-center border border-border bg-muted text-foreground ${SIZES[size]} ${className}`} aria-hidden>
      <svg viewBox="0 0 24 24" className="size-full">
        <Mark kind={kind} color={color} />
      </svg>
    </span>
  );
}
