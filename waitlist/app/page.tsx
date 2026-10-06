import { ArrowUpRightIcon, BoxesIcon, PlugIcon, ReceiptIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Pricing } from "@/components/pricing";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { SignupForm } from "@/components/signup-form";
import { Terminal } from "@/components/terminal";
import { GITHUB_URL } from "@/lib/site";

const cards = [
  {
    icon: BoxesIcon,
    title: "Pick your services",
    body: "Postgres, Redis and object storage today. Vector, NoSQL, auth, realtime and queues next.",
  },
  {
    icon: PlugIcon,
    title: "Native, not locked in",
    body: "Standard connection strings that work with any driver or ORM. The SDK and API are extras, not a wall.",
  },
  {
    icon: ReceiptIcon,
    title: "Pay per resource",
    body: "No bundles and no surprise bills. Every resource has its own Free, Pro and Max plan, with hard caps and a clear upgrade prompt.",
  },
];

const steps: { title: string; body: ReactNode }[] = [
  { title: "Create a project", body: "Free to create. Give it a name and you're set." },
  { title: "Add resources", body: "Pick a service and a plan: Postgres, a Redis cache, or object storage." },
  {
    title: "Connect",
    body: (
      <>
        Use a standard connection string, the SDK, or the HTTP API. Run <Code>sparkbase env pull</Code> to write
        your .env file.
      </>
    ),
  },
];

const AVAILABLE = "Available at launch";
const LATER = "Coming later";

const resources = [
  { name: "Postgres", what: "A relational database with native connection strings, pooled and direct.", status: AVAILABLE },
  { name: "Cache", what: "A Redis cache over TLS.", status: AVAILABLE },
  { name: "Object storage", what: "S3-compatible buckets with presigned upload and download URLs.", status: AVAILABLE },
  { name: "Vector database", what: "Vector search for AI features.", status: LATER },
  { name: "NoSQL", what: "MongoDB-compatible document storage.", status: LATER },
  { name: "MySQL and MariaDB", what: "More relational engines.", status: LATER },
  { name: "Auth", what: "Built-in authentication.", status: LATER },
  { name: "Realtime", what: "WebSocket subscriptions.", status: LATER },
  { name: "Queues", what: "Background job queues.", status: LATER },
];

const faqs: { q: string; a: ReactNode }[] = [
  {
    q: "When can I get access?",
    a: "We'll start with a small private beta and invite people from the waitlist in batches. You'll get one email when your invite is ready.",
  },
  {
    q: "Is there a free plan?",
    a: "Yes. Every resource has a Free plan. Free and paid plans have hard caps, so you never get a surprise bill.",
  },
  {
    q: "Which databases are supported?",
    a: "Postgres first. MySQL and MariaDB come later, along with MongoDB and a vector database.",
  },
  {
    q: "Can I use my own driver or ORM?",
    a: "Yes. Every resource gives you a standard connection string. The SDK and API are optional.",
  },
  {
    q: "Can I self-host it?",
    a: (
      <>
        Yes. Sparkbase is open source, and a single <Code>docker compose up</Code> runs it on your own machine.
      </>
    ),
  },
  {
    q: "What's the difference between the waitlist and the newsletter?",
    a: "The waitlist is for early access to the product. The newsletter is for build-in-public updates as we ship. You can join both.",
  },
];

function Code({ children }: { children: ReactNode }) {
  return (
    <code className="rounded-md border border-border bg-card px-1.5 py-0.5 font-mono text-[0.85em] text-foreground">
      {children}
    </code>
  );
}

function Section({ id, labelledBy, children }: { id?: string; labelledBy: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={labelledBy} className="border-t border-border">
      <div className="mx-auto max-w-6xl px-5 py-24 sm:px-8 md:py-32">{children}</div>
    </section>
  );
}

function H2({ id, children }: { id: string; children: ReactNode }) {
  return (
    <h2 id={id} className="text-4xl font-medium tracking-[-0.04em] text-balance md:text-6xl">
      {children}
    </h2>
  );
}

export default function Home() {
  return (
    <>
      {/* 1. Hero */}
      <section aria-labelledby="hero-title" className="mx-auto max-w-6xl px-5 pt-16 pb-24 sm:px-8 md:pt-24 md:pb-32">
        <div className="mx-auto max-w-3xl text-center">
          <a
            href={GITHUB_URL}
            rel="noopener"
            className="group inline-flex max-w-full items-center gap-2 rounded-full bg-card py-1.5 pr-1.5 pl-4 text-sm transition-colors hover:bg-accent sm:pl-5 sm:text-[15px]"
          >
            <span className="font-semibold">Open source</span>
            <span aria-hidden="true" className="text-muted-foreground">·</span>
            <span className="truncate">Self-hostable</span>
            <span aria-hidden="true" className="hidden text-muted-foreground sm:inline">·</span>
            <span className="hidden sm:inline">Early access coming soon</span>
            <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-accent transition-colors group-hover:bg-brand group-hover:text-on-brand sm:size-8">
              <ArrowUpRightIcon aria-hidden="true" className="size-4" />
            </span>
          </a>
          <h1
            id="hero-title"
            className="mt-8 text-5xl leading-[1.02] font-medium tracking-[-0.05em] text-balance sm:text-6xl md:text-[5.5rem]"
          >
            The data layer for your backend.
          </h1>
          <p className="mx-auto mt-6 max-w-2xl text-lg leading-relaxed text-pretty text-muted-foreground md:text-xl">
            Create a project. Attach only the services you need. Connect with a normal connection string, an SDK, or an
            API. Pay for exactly what you attach.
          </p>
          <div className="mt-10">
            <SignupForm id="join" />
          </div>
        </div>

        <div className="mx-auto mt-20 max-w-4xl">
          <Terminal />
        </div>
      </section>

      {/* 2. Only what you need */}
      <Section labelledBy="only-title">
        <div className="max-w-2xl">
          <H2 id="only-title">Only what you need.</H2>
          <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
            Most platforms hand you a bundle. Sparkbase gives you a project, and you attach only the services your
            backend actually uses.
          </p>
        </div>
        <ul className="mt-14 grid gap-4 md:grid-cols-3">
          {cards.map((card) => {
            const Icon = card.icon;
            return (
              <li key={card.title}>
                <Card className="h-full gap-0 rounded-3xl border bg-card py-0 ring-0">
                  <CardContent className="p-8">
                    <span className="flex size-10 items-center justify-center rounded-xl bg-accent">
                      <Icon aria-hidden="true" className="size-5 text-foreground" />
                    </span>
                    <h3 className="mt-8 text-xl font-semibold tracking-tight">{card.title}</h3>
                    <p className="mt-3 leading-relaxed text-muted-foreground">{card.body}</p>
                  </CardContent>
                </Card>
              </li>
            );
          })}
        </ul>
      </Section>

      {/* 3. How it works */}
      <Section id="how-it-works" labelledBy="how-title">
        <H2 id="how-title">From zero to connected in three steps.</H2>
        <ol className="mt-14 grid gap-10 md:grid-cols-3 md:gap-8">
          {steps.map((step, i) => (
            <li key={step.title} className="border-t pt-6">
              <span
                className={cn("font-mono text-sm", i === steps.length - 1 ? "text-brand" : "text-muted-foreground")}
              >
                0{i + 1}
              </span>
              <h3 className="mt-4 text-xl font-semibold tracking-tight">{step.title}</h3>
              <p className="mt-3 leading-relaxed text-muted-foreground">{step.body}</p>
            </li>
          ))}
        </ol>
      </Section>

      {/* 4. Resources */}
      <Section id="resources" labelledBy="resources-title">
        <div className="max-w-2xl">
          <H2 id="resources-title">Resources</H2>
          <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
            Each resource is its own service, with its own plan.
          </p>
        </div>
        <div className="mt-14 overflow-hidden rounded-3xl border">
          <Table className="min-w-[36rem]">
            <TableHeader className="bg-card">
              <TableRow className="hover:bg-transparent">
                <TableHead className="h-12 px-6 font-mono text-xs tracking-wide text-muted-foreground uppercase">
                  Resource
                </TableHead>
                <TableHead className="h-12 px-6 font-mono text-xs tracking-wide text-muted-foreground uppercase">
                  What you get
                </TableHead>
                <TableHead className="h-12 px-6 font-mono text-xs tracking-wide text-muted-foreground uppercase">
                  Status
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {resources.map((r) => (
                <TableRow key={r.name} className="hover:bg-card/60">
                  <TableHead scope="row" className="h-auto px-6 py-5 text-[15px] font-semibold text-foreground">
                    {r.name}
                  </TableHead>
                  <TableCell className="px-6 py-5 text-[15px] leading-relaxed whitespace-normal text-muted-foreground">
                    {r.what}
                  </TableCell>
                  <TableCell className="px-6 py-5">
                    <Badge
                      variant="outline"
                      className={cn(
                        "h-7 gap-2 rounded-full px-3 text-[13px] font-normal",
                        r.status === AVAILABLE ? "text-foreground" : "text-muted-foreground",
                      )}
                    >
                      <span
                        aria-hidden="true"
                        className={cn(
                          "size-2 rounded-full",
                          r.status === AVAILABLE ? "bg-brand" : "bg-muted-foreground/60",
                        )}
                      />
                      {r.status}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Section>

      {/* Pricing */}
      <Section id="pricing" labelledBy="pricing-title">
        <div className="mx-auto max-w-2xl text-center">
          <H2 id="pricing-title">Pricing</H2>
        </div>
        <div className="mt-10">
          <Pricing />
        </div>
      </Section>

      {/* 5. Open source */}
      <Section id="open-source" labelledBy="oss-title">
        <div className="grid items-center gap-12 md:grid-cols-2">
          <div>
            <H2 id="oss-title">Open source and self-hostable.</H2>
            <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
              The same code runs our cloud and your laptop. Self-host the whole thing with no feature gating, or let us
              run it for you: managed hosts, backups, upgrades and support.
            </p>
            <Button asChild size="pill" className="mt-8">
              <a href={GITHUB_URL} rel="noopener">
                View on GitHub
                <ArrowUpRightIcon aria-hidden="true" data-icon="inline-end" />
              </a>
            </Button>
          </div>
          <pre className="overflow-x-auto rounded-3xl border border-term-line bg-term-bg px-6 py-8 font-mono text-sm text-term-fg">
            <code>
              <span className="text-term-muted select-none">$ </span>docker compose up
            </code>
          </pre>
        </div>
      </Section>

      {/* 6. FAQ */}
      <Section labelledBy="faq-title">
        <div className="grid gap-10 md:grid-cols-[1fr_1.4fr] md:gap-16">
          {/* TODO(copy): the copy deck has no FAQ heading; "FAQ" is the section name from the deck. */}
          <H2 id="faq-title">FAQ</H2>
          <Accordion type="single" collapsible className="border-t">
            {faqs.map((faq) => (
              <AccordionItem key={faq.q} value={faq.q} className="border-b last:border-b">
                <AccordionTrigger className="items-center gap-6 rounded-none py-6 text-lg font-medium hover:no-underline focus-visible:ring-0">
                  {faq.q}
                </AccordionTrigger>
                <AccordionContent className="pr-10 pb-6 text-base leading-relaxed text-muted-foreground">
                  {faq.a}
                </AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
        </div>
      </Section>

      {/* 7. Final CTA */}
      <Section labelledBy="cta-title">
        <div className="mx-auto max-w-2xl text-center">
          <H2 id="cta-title">Get early access.</H2>
          <p className="mt-5 text-lg leading-relaxed text-pretty text-muted-foreground">
            Join the waitlist and we&apos;ll email you once when it opens. Prefer to follow along? Switch to the
            newsletter.
          </p>
          <div className="mt-10">
            <SignupForm />
          </div>
        </div>
      </Section>
    </>
  );
}
