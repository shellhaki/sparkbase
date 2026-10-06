"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { KIND_LABEL, api } from "@/lib/api";
import { PLATFORM_KINDS, type Resource } from "@/lib/kinds";
import { ServiceLogo } from "./logos/service-logo";
import { Card, CardHeader } from "./ui";

const NATIVE = ["postgres", "vector", "cache", "storage"];
const NODE_W = 210;
const ROW = 74;

interface Placed {
  r: Resource;
  x: number;
  y: number;
  h: number;
  addons: string[];
}

/** How a project fits together: who calls what, and what each database has inside. */
export function Architecture({ projectRef, resources }: { projectRef: string; resources: Resource[] }) {
  const router = useRouter();
  const [addons, setAddons] = useState<Record<string, string[]>>({});

  // Installed extensions are the add-ons inside each Postgres service.
  useEffect(() => {
    resources
      .filter((r) => r.kind === "postgres" && r.status === "active")
      .forEach((r) => {
        api<{ installed: Array<{ name: string }> }>("GET", `/p/${projectRef}/postgres/${r.name}/extensions`)
          .then((d) => setAddons((a) => ({ ...a, [r.id]: d.installed.map((x) => x.name) })))
          .catch(() => {});
      });
  }, [projectRef, resources]);

  const data = resources.filter((r) => !PLATFORM_KINDS.includes(r.kind));
  const platform = resources.filter((r) => PLATFORM_KINDS.includes(r.kind));

  // Columns: clients, gateway, data services, platform services.
  const colX = [20, 290, 560, 860];
  const placeColumn = (list: Resource[], x: number): Placed[] => {
    let y = 20;
    return list.map((r) => {
      const a = addons[r.id] ?? [];
      const h = r.kind === "postgres" && a.length ? 56 + Math.ceil(a.length / 3) * 22 : 56;
      const p = { r, x, y, h, addons: a };
      y += h + (ROW - 56);
      return p;
    });
  };
  const dataNodes = placeColumn(data, colX[2]);
  const platformNodes = placeColumn(platform, colX[3]);
  const bottom = Math.max(260, ...[...dataNodes, ...platformNodes].map((n) => n.y + n.h + 20));
  const width = platform.length ? colX[3] + NODE_W + 20 : colX[2] + NODE_W + 20;
  const hasNative = data.some((r) => NATIVE.includes(r.kind));

  const clients = [
    { id: "browser", label: "Browser apps", sub: "publishable key" },
    { id: "server", label: "Your server", sub: "secret key" },
    ...(hasNative ? [{ id: "native", label: "Drivers & tools", sub: "native connections" }] : []),
  ].map((c, i) => ({ ...c, x: colX[0], y: 20 + i * 90, h: 56 }));
  const api_ = { x: colX[1], y: 20, h: 76 };
  const edge = { x: colX[1], y: 150, h: 56 };

  const curve = (x1: number, y1: number, x2: number, y2: number) => {
    const mx = (x1 + x2) / 2;
    return `M${x1} ${y1} C${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
  };
  const byName = new Map(dataNodes.map((n) => [n.r.name, n]));

  return (
    <Card className="overflow-hidden">
      <CardHeader title="Architecture" />
      <div className="overflow-x-auto p-4">
        <svg viewBox={`0 0 ${width} ${bottom}`} style={{ minWidth: Math.min(width, 900) }} className="w-full" role="img" aria-label="Project architecture">
          <defs>
            <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
              <path d="M0 0L10 5L0 10z" fill="var(--muted-foreground)" />
            </marker>
          </defs>

          {/* Edges */}
          <g fill="none" strokeWidth="1.4">
            {clients
              .filter((c) => c.id !== "native")
              .map((c) => (
                <path key={c.id} d={curve(c.x + 200, c.y + 28, api_.x, api_.y + 38)} stroke="var(--border)" markerEnd="url(#arrow)" />
              ))}
            {hasNative && <path d={curve(clients[2].x + 200, clients[2].y + 28, edge.x, edge.y + 28)} stroke="var(--border)" markerEnd="url(#arrow)" />}
            {[...dataNodes, ...platformNodes].map((n) => (
              <path
                key={"api" + n.r.id}
                d={curve(api_.x + NODE_W, api_.y + 38, n.x, n.y + 28)}
                stroke={n.r.status === "active" ? "var(--brand)" : "var(--border)"}
                strokeOpacity={n.r.runtime === "container" && n.r.awake === false ? 0.35 : 0.7}
                strokeDasharray={n.r.runtime === "container" && n.r.awake === false ? "4 4" : undefined}
              />
            ))}
            {hasNative &&
              dataNodes
                .filter((n) => NATIVE.includes(n.r.kind))
                .map((n) => <path key={"edge" + n.r.id} d={curve(edge.x + NODE_W, edge.y + 28, n.x, n.y + 40)} stroke="var(--border)" strokeDasharray="2 4" />)}
            {platformNodes.flatMap((n) =>
              (n.r as Resource & { links?: string[] }).links?.map((name) => {
                const t = byName.get(name);
                if (!t) return null;
                return <path key={n.r.id + name} d={curve(n.x, n.y + 28, t.x + NODE_W, t.y + 28)} stroke="var(--brand)" strokeOpacity="0.5" strokeDasharray="5 4" markerEnd="url(#arrow)" />;
              }) ?? [],
            )}
          </g>

          {/* Clients */}
          {clients.map((c) => (
            <g key={c.id}>
              <rect x={c.x} y={c.y} width={200} height={c.h} rx="12" fill="var(--card)" stroke="var(--border)" />
              <text x={c.x + 16} y={c.y + 24} className="fill-foreground text-[13px] font-medium">
                {c.label}
              </text>
              <text x={c.x + 16} y={c.y + 42} className="fill-muted-foreground font-mono text-[11px]">
                {c.sub}
              </text>
            </g>
          ))}

          {/* Gateway */}
          <g>
            <rect x={api_.x} y={api_.y} width={NODE_W} height={api_.h} rx="12" fill="var(--card)" stroke="var(--brand)" />
            <text x={api_.x + 16} y={api_.y + 26} className="fill-foreground text-[13px] font-semibold">
              Project API
            </text>
            <text x={api_.x + 16} y={api_.y + 46} className="fill-muted-foreground font-mono text-[11px]">
              /p/{projectRef}
            </text>
            <text x={api_.x + 16} y={api_.y + 62} className="fill-muted-foreground font-mono text-[10px]">
              keys · rate limits · logs
            </text>
          </g>
          {hasNative && (
            <g>
              <rect x={edge.x} y={edge.y} width={NODE_W} height={edge.h} rx="12" fill="var(--card)" stroke="var(--border)" />
              <text x={edge.x + 16} y={edge.y + 24} className="fill-foreground text-[13px] font-medium">
                Edge (TLS)
              </text>
              <text x={edge.x + 16} y={edge.y + 42} className="fill-muted-foreground font-mono text-[11px]">
                wakes sleeping services
              </text>
            </g>
          )}

          {/* Services */}
          {[...dataNodes, ...platformNodes].map((n) => (
            <g key={n.r.id} className="cursor-pointer" onClick={() => router.push(`/projects/${n.r.projectId}/services/${n.r.id}`)}>
              <rect x={n.x} y={n.y} width={NODE_W} height={n.h} rx="12" fill="var(--card)" stroke="var(--border)" />
              <foreignObject x={n.x + 10} y={n.y + 10} width="36" height="36">
                <ServiceLogo kind={n.r.kind} size="sm" className="!size-9" />
              </foreignObject>
              <text x={n.x + 56} y={n.y + 25} className="fill-foreground text-[13px] font-medium">
                {n.r.name}
              </text>
              <text x={n.x + 56} y={n.y + 42} className="fill-muted-foreground font-mono text-[11px]">
                {KIND_LABEL[n.r.kind]}
                {n.r.runtime === "container" ? (n.r.awake ? " · awake" : " · asleep") : ""}
                {n.r.status !== "active" ? ` · ${n.r.status}` : ""}
              </text>
              <circle cx={n.x + NODE_W - 14} cy={n.y + 14} r="3.5" fill={n.r.status === "active" ? (n.r.awake === false ? "var(--muted-foreground)" : "var(--success)") : "var(--warning)"} />
              {n.addons.map((a, i) => {
                const col = i % 3;
                const row = Math.floor(i / 3);
                const ax = n.x + 10 + col * 64;
                const ay = n.y + 54 + row * 22;
                return (
                  <g key={a}>
                    <rect x={ax} y={ay} width="60" height="17" rx="5" fill="var(--muted)" />
                    <text x={ax + 30} y={ay + 12} textAnchor="middle" className="fill-muted-foreground font-mono text-[9.5px]">
                      {a.length > 10 ? a.slice(0, 9) + "…" : a}
                    </text>
                  </g>
                );
              })}
            </g>
          ))}
          {resources.length === 0 && (
            <text x={colX[2]} y={60} className="fill-muted-foreground text-[13px]">
              Add a service to see it here.
            </text>
          )}
        </svg>
      </div>
    </Card>
  );
}
