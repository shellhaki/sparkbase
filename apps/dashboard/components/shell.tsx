"use client";

import { Bell, FolderKanban, KeyRound, LogOut, Menu, Search, Shield, X } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, timeAgo } from "@/lib/api";
import { CommandPalette, openCommandPalette } from "./command-palette";
import { Toaster } from "./toast";
import { cn } from "./ui";

export interface Me {
  id: string;
  email: string;
  name: string | null;
  role: "admin" | "member";
}

const SessionContext = createContext<{ user: Me; refresh: () => Promise<void> } | null>(null);

export function useSession() {
  const s = useContext(SessionContext);
  if (!s) throw new Error("useSession outside the app shell");
  return s;
}

export function initials(user: Me) {
  const src = (user.name ?? user.email).trim();
  const parts = src.split(/[\s@._-]+/).filter(Boolean);
  return ((parts[0]?.[0] ?? "") + (parts[1]?.[0] ?? "")).toUpperCase() || "?";
}

/** A gradient avatar with initials, stable per email. */
export function Avatar({ user, size = 32 }: { user: Me; size?: number }) {
  let h = 0;
  for (const ch of user.email) h = (h * 31 + ch.charCodeAt(0)) % 360;
  return (
    <span
      className="inline-flex shrink-0 items-center justify-center rounded-full font-display text-xs font-semibold text-white"
      style={{ width: size, height: size, background: `linear-gradient(135deg, hsl(${h} 75% 55%), hsl(${(h + 60) % 360} 80% 45%))` }}
      aria-hidden
    >
      {initials(user)}
    </span>
  );
}

interface Notification {
  id: string;
  level: string;
  message: string;
  readAt: string | null;
  createdAt: string;
}

function Notifications() {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const load = () =>
    api<{ notifications: Notification[] }>("GET", "/v1/notifications?limit=20")
      .then((d) => setItems(d.notifications))
      .catch(() => {});
  useEffect(() => {
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);
  const unread = items.filter((n) => !n.readAt).length;
  return (
    <div className="relative">
      <button
        onClick={async () => {
          setOpen(!open);
          if (!open && unread > 0) {
            await api("POST", "/v1/notifications/read").catch(() => {});
            load();
          }
        }}
        className="relative flex size-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
        aria-label={`Notifications${unread ? `, ${unread} unread` : ""}`}
      >
        <Bell className="size-4" />
        {unread > 0 && <span className="led absolute top-2 right-2 !size-1.5 text-brand" />}
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div className="animate-pop-in absolute right-0 z-40 mt-2 w-80 origin-top-right overflow-hidden rounded-xl border border-border bg-card shadow-2xl">
            <div className="border-b border-border px-4 py-2.5 font-mono text-xs tracking-wide text-muted-foreground uppercase">Signals</div>
            <div className="max-h-96 overflow-y-auto">
              {items.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted-foreground">All quiet.</p>}
              {items.map((n) => (
                <div key={n.id} className="flex gap-3 border-b border-border px-4 py-3 last:border-0">
                  <span className={cn("led mt-1.5 !size-1.5 shrink-0", n.level === "critical" ? "text-destructive" : n.level === "warning" ? "text-warning" : "text-brand")} />
                  <div className="min-w-0">
                    <p className="text-sm">{n.message}</p>
                    <p className="mt-1 font-mono text-[11px] text-muted-foreground">{timeAgo(n.createdAt)}</p>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </>
      )}
    </div>
  );
}

/** Shown while the session loads: the mark with a soft pulse. */
function BootSplash() {
  return (
    <div className="animate-fade-in flex min-h-dvh items-center justify-center" aria-busy="true" aria-label="Loading Sparkbase">
      <div className="relative flex size-16 items-center justify-center">
        <span className="pulse-ring absolute inset-0 rounded-full bg-brand/30" />
        <Image src="/mark.png" alt="" width={36} height={36} priority className="relative" />
      </div>
    </div>
  );
}

interface ProjectLite {
  id: string;
  name: string;
  serviceKinds?: string[];
}

/** The user's projects in the sidebar, each with its service signal. */
function SidebarProjects({ pathname }: { pathname: string }) {
  const [projects, setProjects] = useState<ProjectLite[]>([]);
  useEffect(() => {
    api<{ projects: ProjectLite[] }>("GET", "/v1/projects")
      .then((d) => setProjects(d.projects.slice(0, 8)))
      .catch(() => {});
  }, [pathname]);
  if (projects.length === 0) return null;
  return (
    <div className="mt-7">
      <div className="px-3 pb-2 font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">Projects</div>
      <div className="space-y-0.5">
        {projects.map((p) => {
          const active = pathname.startsWith(`/projects/${p.id}`);
          const kinds = p.serviceKinds ?? [];
          return (
            <Link
              key={p.id}
              href={`/projects/${p.id}`}
              className={cn(
                "flex items-center gap-2.5 rounded-lg px-3 py-1.5 text-sm transition-colors",
                active ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
              )}
            >
              <span className="flex h-4 w-1.5 shrink-0 flex-col overflow-hidden rounded-full bg-muted">
                {kinds.slice(0, 6).map((k, i) => (
                  <span key={i} className="flex-1 bg-brand" />
                ))}
              </span>
              <span className="truncate">{p.name}</span>
            </Link>
          );
        })}
      </div>
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<Me | null>(null);
  const [navOpen, setNavOpen] = useState(false);
  const [mac, setMac] = useState(true);

  const refresh = async () => {
    try {
      const d = await api<{ user: Me }>("GET", "/v1/me");
      setUser(d.user);
    } catch {
      const s = await api("GET", "/v1/setup").catch(() => ({ open: false }));
      router.replace(s.open ? "/setup" : "/login");
    }
  };

  useEffect(() => {
    refresh();
    setMac(/Mac|iPhone|iPad/.test(navigator.platform));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => setNavOpen(false), [pathname]);

  if (!user) return <BootSplash />;

  const nav = [
    { href: "/", label: "Projects", icon: FolderKanban, active: pathname === "/" },
    { href: "/account", label: "Account", icon: KeyRound, active: pathname.startsWith("/account") },
    ...(user.role === "admin" ? [{ href: "/admin", label: "Admin", icon: Shield, active: pathname.startsWith("/admin") }] : []),
  ];

  return (
    <SessionContext.Provider value={{ user, refresh }}>
      <div className="flex min-h-dvh">
        <aside
          className={cn(
            "fixed inset-y-0 left-0 z-30 flex w-64 flex-col border-r border-border bg-background/95 backdrop-blur transition-transform duration-300 md:sticky md:top-0 md:h-dvh md:translate-x-0",
            navOpen ? "translate-x-0" : "-translate-x-full",
          )}
          style={{ transitionTimingFunction: "var(--ease-out)" }}
        >
          <div className="flex h-16 items-center gap-2.5 px-5">
            <Image src="/mark.png" alt="" width={24} height={24} />
            <span className="font-display text-[15px] font-semibold tracking-tight">Sparkbase</span>
            <button className="ml-auto rounded p-1 md:hidden" onClick={() => setNavOpen(false)} aria-label="Close menu">
              <X className="size-4" />
            </button>
          </div>
          <nav className="flex-1 overflow-y-auto px-3 py-2">
            <div className="space-y-0.5">
              {nav.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  className={cn(
                    "relative flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                    n.active ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                  )}
                >
                  {n.active && <span className="absolute top-2 bottom-2 left-0 w-[3px] rounded-full bg-brand" />}
                  <n.icon className="size-4" />
                  {n.label}
                </Link>
              ))}
            </div>
            <SidebarProjects pathname={pathname} />
          </nav>
          <div className="m-3 flex items-center gap-3 rounded-xl border border-border bg-card/60 p-3">
            <Avatar user={user} size={34} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{user.name ?? user.email.split("@")[0]}</div>
              <div className="truncate font-mono text-[11px] text-muted-foreground">{user.role}</div>
            </div>
            <button
              onClick={async () => {
                await api("POST", "/v1/auth/logout").catch(() => {});
                router.replace("/login");
              }}
              className="rounded-lg p-2 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              aria-label="Sign out"
              title="Sign out"
            >
              <LogOut className="size-4" />
            </button>
          </div>
        </aside>
        {navOpen && <div className="animate-fade-in fixed inset-0 z-20 bg-black/50 md:hidden" onClick={() => setNavOpen(false)} />}
        <div className="relative flex min-w-0 flex-1 flex-col">
          <div className="dot-grid pointer-events-none absolute inset-x-0 top-0 h-[560px]" aria-hidden />
          <header className="sticky top-0 z-10 flex h-16 items-center gap-3 border-b border-border/60 bg-background/70 px-4 backdrop-blur-xl md:px-8">
            <button className="rounded p-2 md:hidden" onClick={() => setNavOpen(true)} aria-label="Open menu">
              <Menu className="size-4" />
            </button>
            <Link href="/" className="flex shrink-0 items-center md:hidden" aria-label="Sparkbase">
              <Image src="/mark.png" alt="" width={20} height={20} />
            </Link>
            <button
              onClick={openCommandPalette}
              className="flex h-9 w-full max-w-sm items-center gap-2 rounded-lg border border-border bg-card/60 px-3 text-sm text-muted-foreground transition-colors hover:border-brand/40 hover:text-foreground"
            >
              <Search className="size-4" />
              <span className="flex-1 text-left">Jump to…</span>
              <kbd className="hidden rounded border border-border px-1.5 font-mono text-[10px] sm:inline">{mac ? "⌘" : "Ctrl"} K</kbd>
            </button>
            <div className="ml-auto">
              <Notifications />
            </div>
          </header>
          <main key={pathname} className="animate-fade-up relative mx-auto w-full max-w-6xl flex-1 px-4 py-10 md:px-8">
            {children}
          </main>
        </div>
      </div>
      <CommandPalette isAdmin={user.role === "admin"} />
      <Toaster />
    </SessionContext.Provider>
  );
}

export function PageHeader({
  title,
  description,
  action,
  back,
  eyebrow,
}: {
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  back?: ReactNode;
  eyebrow?: ReactNode;
}) {
  return (
    <div className="mb-10">
      {back && <div className="mb-4 text-sm text-muted-foreground transition-colors hover:text-foreground">{back}</div>}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          {eyebrow && <div className="mb-2 font-mono text-[11px] tracking-[0.14em] text-muted-foreground uppercase">{eyebrow}</div>}
          <h1 className="font-display text-3xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1.5 text-sm text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
    </div>
  );
}
