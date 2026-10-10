import { ArrowRight, ArrowUpRight, Boxes, Eye, KeyRound, Lock, Moon, Plug, ServerCog, Zap } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { CopyCommand } from "@/components/copy-command";
import { Frame, Shot } from "@/components/frame";
import { ServiceLogo } from "@/components/logos/service-logo";
import { GitHubIcon, SiteNav } from "@/components/site-nav";
import { highlight } from "@/lib/highlight";
import { GITHUB_URL, X_URL } from "@/lib/site";

const DATA = [
  { kind: "postgres", name: "Postgres", body: "A real Postgres 18 database with its own role. SQL over HTTP, or any driver.", native: "postgres://" },
  { kind: "cache", name: "Cache", body: "Redis with its own user and memory limit. Key/value API, or any Redis client.", native: "rediss://" },
  { kind: "storage", name: "Storage", body: "A private bucket. Uploads, downloads and signed URLs, or the S3 API.", native: "s3://" },
  { kind: "vector", name: "Vector", body: "Embeddings with similarity search on pgvector, for AI features.", native: "postgres://" },
  { kind: "queue", name: "Queues", body: "Durable queues with visibility timeouts, retries and dead letters.", native: null },
  { kind: "auth", name: "Auth", body: "Email and password sign-in for your app's users, with JWT sessions.", native: null },
];

const PLATFORM = [
  { kind: "realtime", name: "Realtime", body: "Broadcast channels and live table changes over Server-Sent Events." },
  { kind: "cron", name: "Cron", body: "Scheduled jobs that call a URL, run SQL or send a queue message." },
  { kind: "backup", name: "Backups", body: "Scheduled dumps, encrypted, sent to your own R2 or S3 bucket." },
  { kind: "monitor", name: "Monitor", body: "Uptime checks with alerts and a webhook when something goes down." },
];

const SDK = `import { createClient } from '@sparkbase/sdk'

const sb = createClient(
  process.env.SPARKBASE_URL,
  process.env.SPARKBASE_SECRET_KEY,
)

const { rows } = await sb.postgres('main').query('select * from orders')
await sb.cache('sessions').set('user:42', { name: 'Ada' }, { ttl: 60 })
await sb.storage('uploads').upload('avatars/42.png', file)
await sb.queue('jobs').queue('emails').send({ to: 'ada@acme.dev' })
await sb.realtime('live').broadcast('public:orders', { id: 42 })`;

const NATIVE = `# Postgres: psql, Prisma, Drizzle…
psql "postgres://…@main-a1b2.db.example.com/…"

# Redis: any client
redis-cli -u "rediss://…@sessions-a1b2.cache.example.com"

# Storage: any S3 tool
aws s3 ls --endpoint-url https://s3.example.com

# Or write every URL and key to .env
sparkbase env pull`;

const INSTALL = [
  { title: "Clone", cmd: "git clone https://github.com/shellhaki/sparkbase && cd sparkbase/deploy" },
  { title: "Configure", cmd: "cp .env.example .env && ./generate-secrets.sh" },
  { title: "Start", cmd: "docker compose up -d" },
];

const WHY = [
  { icon: Boxes, title: "Pick your services", body: "A project starts empty. Add only what your app uses, and only that runs." },
  { icon: KeyRound, title: "One URL, one key, one SDK", body: "Every service in a project answers at the project URL with a project key." },
  { icon: Plug, title: "Native when it exists", body: "Postgres, Redis and S3 give you real connection strings. The SDK is a convenience, never a wall." },
  { icon: Lock, title: "Isolated by default", body: "Each service gets its own credentials, role and limits. Isolation tests prove tenants cannot reach each other." },
  { icon: Eye, title: "See everything", body: "Live logs and metrics for every service, a terminal for databases and caches, server logs for admins." },
  { icon: ServerCog, title: "No vendor lock", body: "Any Linux server with Docker. Any S3 bucket. Configuration is environment variables." },
];

const FAQ: Array<{ q: string; a: ReactNode }> = [
  {
    q: "Does it cost anything?",
    a: "No. Sparkbase is free and open source under AGPL-3.0, with no paid tier and no locked features. The SDK and CLI are MIT. You pay only for the server you run it on.",
  },
  {
    q: "What do I need to run it?",
    a: (
      <>
        A Linux server with Docker and a domain. <Code>docker compose up -d</Code> starts everything, and Traefik gets the HTTPS certificates. Each database and cache runs in its own
        container.
      </>
    ),
  },
  {
    q: "How does scale to zero work?",
    a: "Databases and caches stop after a few idle minutes (you choose how many). Data stays on disk. The next connection or API call starts the container again, usually in under half a second, and the client never notices beyond that first wait.",
  },
  {
    q: "Can I use my own driver or ORM?",
    a: "Yes. Postgres, vector, cache and storage give you standard connection strings. Prisma, Drizzle, psql, ioredis and the AWS SDK all work as usual.",
  },
  {
    q: "Can other people use my install?",
    a: "Yes. The first account becomes the admin, and everyone else joins by invite. Each person gets their own projects, with limits the admin sets.",
  },
  {
    q: "Is it ready for production?",
    a: "Not yet. It is an early release: isolation suites, SDK tests and a full Docker install test pass, but there has been no 1.0 and no long production run yet. Keep off-site backups, which the backup service makes easy.",
  },
];

function Code({ children }: { children: ReactNode }) {
  return <code className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-[0.85em] text-foreground">{children}</code>;
}

function Section({ id, children, className = "" }: { id?: string; children: ReactNode; className?: string }) {
  return (
    <section id={id} className={`scroll-mt-16 border-t border-border/60 ${className}`}>
      <div className="mx-auto max-w-6xl px-5 py-24 sm:px-8 md:py-32">{children}</div>
    </section>
  );
}

function Heading({ eyebrow, title, body }: { eyebrow: string; title: ReactNode; body?: ReactNode }) {
  return (
    <div className="max-w-2xl">
      <div className="eyebrow">{eyebrow}</div>
      <h2 className="mt-4 font-display text-4xl font-semibold tracking-[-0.03em] text-balance md:text-5xl">{title}</h2>
      {body && <p className="mt-5 text-lg leading-relaxed text-pretty text-muted-foreground">{body}</p>}
    </div>
  );
}

/** A day of activity for a few services: bars are the hours the container was awake. */
function SleepTimeline() {
  const rows = [
    {
      kind: "postgres",
      name: "main",
      awake: [
        [8, 13],
        [14, 19],
      ],
    },
    {
      kind: "cache",
      name: "sessions",
      awake: [
        [8, 12.5],
        [15, 18],
      ],
    },
    {
      kind: "vector",
      name: "docs",
      awake: [
        [10, 10.6],
        [16.2, 16.8],
      ],
    },
    {
      kind: "queue",
      name: "jobs",
      awake: [
        [3, 3.3],
        [9, 9.4],
        [21, 21.3],
      ],
    },
    { kind: "auth", name: "auth", awake: [] as number[][] },
  ];
  return (
    <div className="rounded-2xl border border-border bg-card p-5 sm:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="eyebrow">Containers · last 24 h</span>
        <span className="flex items-center gap-4 font-mono text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-4 rounded-sm bg-brand" /> awake
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2 w-4 rounded-sm bg-accent" /> asleep, 0 MB
          </span>
        </span>
      </div>
      <div className="mt-6 space-y-4">
        {rows.map((r, i) => (
          <div key={r.name} className="flex items-center gap-3">
            <ServiceLogo kind={r.kind} size="sm" />
            <span className="hidden w-16 font-mono text-xs text-muted-foreground sm:block">{r.name}</span>
            <div className="relative h-3 flex-1 overflow-hidden rounded-full bg-accent">
              {r.awake.map(([a, b]) => (
                <span
                  key={a}
                  className="awake-bar absolute inset-y-0 rounded-full bg-brand"
                  style={{ left: `${(a / 24) * 100}%`, width: `${((b - a) / 24) * 100}%`, animationDelay: `${i * 90}ms` }}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="mt-5 flex justify-between pl-11 font-mono text-[10px] text-muted-foreground sm:pl-[6.75rem]">
        <span>00:00</span>
        <span>06:00</span>
        <span>12:00</span>
        <span>18:00</span>
        <span>24:00</span>
      </div>
    </div>
  );
}

export default async function Home() {
  const [sdkHtml, nativeHtml] = await Promise.all([highlight(SDK, "ts"), highlight(NATIVE, "bash")]);
  const all = [...DATA, ...PLATFORM];

  return (
    <>
      <SiteNav />
      <main>
        {/* Hero */}
        <section className="relative overflow-hidden">
          <div className="hero-grid pointer-events-none absolute inset-0" aria-hidden />
          <div className="relative mx-auto max-w-6xl px-5 pt-20 sm:px-8 md:pt-28">
            <div className="mx-auto max-w-3xl text-center">
              <a
                href={GITHUB_URL}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-border bg-card/80 py-1 pr-1 pl-3.5 text-[13px] text-muted-foreground transition-colors hover:border-brand/50 hover:text-foreground"
              >
                <span className="font-medium text-foreground">Open source</span>
                <span aria-hidden>·</span>
                <span>AGPL-3.0</span>
                <span aria-hidden className="hidden sm:inline">
                  ·
                </span>
                <span className="hidden sm:inline">Early release</span>
                <span className="flex size-6 items-center justify-center rounded-full bg-accent">
                  <ArrowUpRight className="size-3.5" />
                </span>
              </a>
              <h1 className="mt-8 font-display text-5xl leading-[1.02] font-semibold tracking-[-0.04em] text-balance sm:text-6xl md:text-7xl">
                Your backend, on your <span className="text-brand">own server.</span>
              </h1>
              <p className="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-pretty text-muted-foreground md:text-xl">
                Add Postgres, Redis, storage, vectors, queues, auth, realtime, cron, backups and uptime checks to a project. Use them from one SDK, or natively. Databases sleep when nobody
                is using them.
              </p>
              <div className="mt-10 flex flex-col items-center justify-center gap-3 sm:flex-row">
                <Link href="/docs/quickstart" className="flex items-center gap-2 rounded-xl bg-brand px-5 py-3 font-medium text-on-brand transition-colors hover:bg-brand-hover">
                  Get started <ArrowRight className="size-4" />
                </Link>
                <a
                  href={GITHUB_URL}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center gap-2 rounded-xl border border-border bg-card/60 px-5 py-3 font-medium transition-colors hover:border-brand/50"
                >
                  <GitHubIcon /> Star on GitHub
                </a>
              </div>
            </div>
            <div className="relative mx-auto mt-16 max-w-5xl md:mt-20">
              <Frame
                src="/shots/project.png"
                alt="A Sparkbase project with its URL, keys and services"
                width={2880}
                height={1800}
                url="localhost:3000/projects/acme-shop"
                priority
                className="shot-fade"
              />
            </div>
          </div>
        </section>

        {/* Service strip */}
        <section className="border-t border-border/60">
          <div className="mx-auto flex max-w-6xl flex-col items-center gap-6 px-5 py-14 sm:px-8 md:flex-row md:justify-between">
            <p className="text-sm text-muted-foreground">Ten services. Add only the ones you need.</p>
            <div className="flex flex-wrap justify-center gap-2">
              {all.map((s) => (
                <span key={s.kind} title={s.name}>
                  <ServiceLogo kind={s.kind} size="md" />
                </span>
              ))}
            </div>
          </div>
        </section>

        {/* Services */}
        <Section id="services">
          <Heading
            eyebrow="Services"
            title="Everything the backend needs, one click each."
            body="Every service gets its own credentials, limits, logs and metrics. Data services hold your data; platform services do the work around it."
          />
          <div className="mt-14 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {DATA.map((s) => (
              <div key={s.kind} className="lift rounded-2xl border border-border bg-card p-6 hover:border-brand/40">
                <div className="flex items-center justify-between">
                  <ServiceLogo kind={s.kind} />
                  {s.native ? (
                    <span className="rounded-md border border-border px-2 py-0.5 font-mono text-[11px] text-muted-foreground">{s.native}</span>
                  ) : (
                    <span className="font-mono text-[11px] text-muted-foreground">API</span>
                  )}
                </div>
                <h3 className="mt-5 font-display text-lg font-semibold">{s.name}</h3>
                <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{s.body}</p>
              </div>
            ))}
          </div>
          <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {PLATFORM.map((s) => (
              <div key={s.kind} className="lift rounded-2xl border border-border bg-card/60 p-6 hover:border-brand/40">
                <ServiceLogo kind={s.kind} />
                <h3 className="mt-5 font-display text-lg font-semibold">{s.name}</h3>
                <p className="mt-1.5 text-sm leading-relaxed text-muted-foreground">{s.body}</p>
              </div>
            ))}
          </div>
        </Section>

        {/* Scale to zero */}
        <Section id="scale-to-zero">
          <div className="grid items-center gap-14 lg:grid-cols-2 [&>*]:min-w-0">
            <div>
              <Heading
                eyebrow="Scale to zero"
                title="Memory only while it's in use."
                body="Each database and cache runs in its own container with a memory limit. When nobody has used it for a few minutes, it stops. The next connection or API call starts it again."
              />
              <dl className="mt-10 grid grid-cols-3 gap-6">
                {[
                  { icon: Moon, k: "0 MB", v: "while asleep" },
                  { icon: Zap, k: "~400 ms", v: "to wake on connect" },
                  { icon: Boxes, k: "1", v: "container per service" },
                ].map(({ icon: Icon, k, v }) => (
                  <div key={v}>
                    <Icon className="size-4 text-brand" />
                    <dt className="mt-3 font-display text-xl font-semibold tracking-tight whitespace-nowrap sm:text-2xl">{k}</dt>
                    <dd className="mt-1 text-sm text-muted-foreground">{v}</dd>
                  </div>
                ))}
              </dl>
            </div>
            <SleepTimeline />
          </div>
        </Section>

        {/* Code */}
        <Section>
          <Heading
            eyebrow="Connect"
            title="One SDK, or the driver you already use."
            body="The SDK covers every service over HTTPS with the project URL and a key. Services with a protocol hand out a real connection string too."
          />
          <div className="mt-14 grid gap-4 lg:grid-cols-12 [&>*]:min-w-0">
            <div className="code-surface overflow-hidden rounded-2xl border border-border bg-code lg:col-span-7">
              <div className="flex items-center justify-between border-b border-border px-5 py-3">
                <span className="font-mono text-[11px] text-muted-foreground">app.ts</span>
                <span className="font-mono text-[11px] text-muted-foreground">npm install @sparkbase/sdk</span>
              </div>
              <div dangerouslySetInnerHTML={{ __html: sdkHtml }} />
            </div>
            <div className="code-surface overflow-hidden rounded-2xl border border-border bg-code lg:col-span-5">
              <div className="border-b border-border px-5 py-3 font-mono text-[11px] text-muted-foreground">native</div>
              <div dangerouslySetInnerHTML={{ __html: nativeHtml }} />
            </div>
          </div>
        </Section>

        {/* Observability */}
        <Section>
          <Heading
            eyebrow="See everything"
            title="Live logs, metrics and a terminal for every service."
            body="Watch requests as they happen, look inside a database, and see how the whole project fits together."
          />
          <div className="mt-14 grid gap-4 lg:grid-cols-2">
            <figure className="min-w-0">
              <Shot src="/shots/logs.png" alt="Live logs for a Postgres service" width={1720} height={1046} cover />
              <figcaption className="mt-3 text-sm text-muted-foreground">Live logs: every API call and the database's own output, with secrets removed.</figcaption>
            </figure>
            <figure className="min-w-0">
              <Shot src="/shots/terminal.png" alt="A psql-like terminal in the dashboard" width={1720} height={924} cover />
              <figcaption className="mt-3 text-sm text-muted-foreground">A small terminal that runs as the service's own role, never as an admin.</figcaption>
            </figure>
          </div>
          <figure className="mt-4">
            <Shot
              src="/shots/architecture.png"
              alt="Project architecture: clients, the project API, services and their add-ons"
              width={2176}
              height={1112}
              sizes="(min-width: 1280px) 1200px, 100vw"
            />
            <figcaption className="mt-3 text-sm text-muted-foreground">
              The architecture view: who calls what, which extensions each database has, and which services wake on connect.
            </figcaption>
          </figure>
        </Section>

        {/* Why */}
        <Section>
          <Heading eyebrow="Why Sparkbase" title="Supabase-style convenience, on a server you already pay for." />
          <div className="mt-14 grid gap-x-10 gap-y-12 sm:grid-cols-2 lg:grid-cols-3">
            {WHY.map(({ icon: Icon, title, body }) => (
              <div key={title}>
                <span className="flex size-10 items-center justify-center rounded-xl border border-border bg-card">
                  <Icon className="size-4.5 text-brand" />
                </span>
                <h3 className="mt-5 font-display text-lg font-semibold">{title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{body}</p>
              </div>
            ))}
          </div>
        </Section>

        {/* Install */}
        <Section id="install">
          <div className="grid gap-14 lg:grid-cols-2 [&>*]:min-w-0">
            <Heading
              eyebrow="Install"
              title="Three commands to your own backend."
              body={
                <>
                  Any Linux server with Docker and a domain. Traefik handles HTTPS. Then open <Code>app.your-domain/setup</Code>: the first account becomes the admin.
                </>
              }
            />
            <ol className="min-w-0 space-y-5">
              {INSTALL.map((s, i) => (
                <li key={s.title} className="flex gap-4">
                  <span className="flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-card font-mono text-xs text-brand">{i + 1}</span>
                  <div className="min-w-0 flex-1">
                    <div className="mb-2 text-sm font-medium">{s.title}</div>
                    <CopyCommand command={s.cmd} />
                  </div>
                </li>
              ))}
              <li className="pl-11">
                <Link href="/docs/self-hosting" className="inline-flex items-center gap-1.5 text-sm text-brand hover:underline">
                  Full self-hosting guide <ArrowRight className="size-3.5" />
                </Link>
              </li>
            </ol>
          </div>
        </Section>

        {/* FAQ */}
        <Section>
          <div className="grid gap-14 lg:grid-cols-3">
            <Heading eyebrow="Questions" title="Good to know." />
            <div className="divide-y divide-border border-y border-border lg:col-span-2">
              {FAQ.map((f) => (
                <details key={f.q} className="group py-5">
                  <summary className="flex items-center justify-between gap-6 font-medium">
                    {f.q}
                    <span className="faq-plus flex size-6 shrink-0 items-center justify-center rounded-full border border-border text-muted-foreground">+</span>
                  </summary>
                  <div className="mt-3 pr-10 text-sm leading-relaxed text-muted-foreground">{f.a}</div>
                </details>
              ))}
            </div>
          </div>
        </Section>

        {/* Final call */}
        <section className="border-t border-border/60">
          <div className="mx-auto max-w-6xl px-5 py-24 text-center sm:px-8 md:py-32">
            <h2 className="mx-auto max-w-2xl font-display text-4xl font-semibold tracking-[-0.03em] text-balance md:text-5xl">Run your backend on your own terms.</h2>
            <div className="mt-10 flex flex-col items-center justify-center gap-3 sm:flex-row">
              <Link href="/docs/quickstart" className="flex items-center gap-2 rounded-xl bg-brand px-5 py-3 font-medium text-on-brand transition-colors hover:bg-brand-hover">
                Read the quickstart <ArrowRight className="size-4" />
              </Link>
              <a
                href={GITHUB_URL}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-2 rounded-xl border border-border bg-card/60 px-5 py-3 font-medium transition-colors hover:border-brand/50"
              >
                <GitHubIcon /> shellhaki/sparkbase
              </a>
            </div>
          </div>
        </section>
      </main>

      <footer className="border-t border-border/60">
        <div className="mx-auto flex max-w-6xl flex-col gap-6 px-5 py-10 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between sm:px-8">
          <span>Sparkbase · server AGPL-3.0, SDK and CLI MIT</span>
          <nav className="flex gap-5">
            <Link href="/docs/introduction" className="hover:text-foreground">
              Docs
            </Link>
            <a href={GITHUB_URL} target="_blank" rel="noreferrer" className="hover:text-foreground">
              GitHub
            </a>
            <a href={X_URL} target="_blank" rel="noreferrer" className="hover:text-foreground">
              X
            </a>
          </nav>
        </div>
      </footer>
    </>
  );
}
