"use client";

import { Bell, FolderKanban, KeyRound, LogOut, Menu, Shield, X } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, timeAgo } from "@/lib/api";
import { Spinner, cn } from "./ui";

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
        className="relative rounded-lg p-2 text-muted-foreground hover:bg-accent hover:text-foreground"
        aria-label={`Notifications${unread ? `, ${unread} unread` : ""}`}
      >
        <Bell className="size-4" />
        {unread > 0 && <span className="absolute top-1 right-1 size-2 rounded-full bg-brand" />}
      </button>
      {open && (
        <div className="absolute right-0 z-40 mt-2 w-80 rounded-xl border border-border bg-card shadow-xl">
          <div className="border-b border-border px-4 py-2.5 text-sm font-medium">Notifications</div>
          <div className="max-h-96 overflow-y-auto">
            {items.length === 0 && <p className="px-4 py-6 text-center text-sm text-muted-foreground">Nothing yet.</p>}
            {items.map((n) => (
              <div key={n.id} className="border-b border-border px-4 py-3 last:border-0">
                <p className={cn("text-sm", n.level === "critical" && "text-destructive", n.level === "warning" && "text-warning")}>{n.message}</p>
                <p className="mt-1 text-xs text-muted-foreground">{timeAgo(n.createdAt)}</p>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [user, setUser] = useState<Me | null>(null);
  const [navOpen, setNavOpen] = useState(false);

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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  useEffect(() => setNavOpen(false), [pathname]);

  if (!user) return <Spinner />;

  const nav = [
    { href: "/", label: "Projects", icon: FolderKanban, active: pathname === "/" || pathname.startsWith("/projects") },
    { href: "/account", label: "Account", icon: KeyRound, active: pathname.startsWith("/account") },
    ...(user.role === "admin" ? [{ href: "/admin", label: "Admin", icon: Shield, active: pathname.startsWith("/admin") }] : []),
  ];

  return (
    <SessionContext.Provider value={{ user, refresh }}>
      <div className="flex min-h-dvh">
        <aside
          className={cn(
            "fixed inset-y-0 left-0 z-30 flex w-60 flex-col border-r border-border bg-background transition-transform md:static md:translate-x-0",
            navOpen ? "translate-x-0" : "-translate-x-full",
          )}
        >
          <div className="flex h-14 items-center gap-2 border-b border-border px-4">
            <Image src="/mark.png" alt="" width={22} height={22} />
            <span className="font-display font-semibold">Sparkbase</span>
            <button className="ml-auto rounded p-1 md:hidden" onClick={() => setNavOpen(false)} aria-label="Close menu">
              <X className="size-4" />
            </button>
          </div>
          <nav className="flex-1 space-y-1 p-3">
            {nav.map((n) => (
              <Link
                key={n.href}
                href={n.href}
                className={cn(
                  "flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                  n.active ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                )}
              >
                <n.icon className="size-4" />
                {n.label}
              </Link>
            ))}
          </nav>
          <div className="border-t border-border p-3">
            <div className="truncate px-3 text-sm font-medium">{user.name ?? user.email}</div>
            <div className="truncate px-3 text-xs text-muted-foreground">{user.email}</div>
            <button
              onClick={async () => {
                await api("POST", "/v1/auth/logout").catch(() => {});
                router.replace("/login");
              }}
              className="mt-2 flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              <LogOut className="size-4" /> Sign out
            </button>
          </div>
        </aside>
        {navOpen && <div className="fixed inset-0 z-20 bg-black/50 md:hidden" onClick={() => setNavOpen(false)} />}
        <div className="flex min-w-0 flex-1 flex-col">
          <header className="flex h-14 items-center gap-2 border-b border-border px-4">
            <button className="rounded p-2 md:hidden" onClick={() => setNavOpen(true)} aria-label="Open menu">
              <Menu className="size-4" />
            </button>
            <div className="ml-auto">
              <Notifications />
            </div>
          </header>
          <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8 md:px-8">{children}</main>
        </div>
      </div>
    </SessionContext.Provider>
  );
}

export function PageHeader({ title, description, action, back }: { title: ReactNode; description?: ReactNode; action?: ReactNode; back?: ReactNode }) {
  return (
    <div className="mb-8">
      {back && <div className="mb-3 text-sm text-muted-foreground">{back}</div>}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0">
          <h1 className="font-display text-2xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
    </div>
  );
}
