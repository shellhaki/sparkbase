import type { Metadata } from "next";
import type { ReactNode } from "react";
import { PageShell } from "@/components/page-shell";
import { contactEmail, PLAUSIBLE_DOMAIN } from "@/lib/site";

export const metadata: Metadata = {
  title: "Privacy",
  alternates: { canonical: "/privacy" },
};

// Evaluated when the page is prerendered, so this is the build date.
const LAST_UPDATED = new Intl.DateTimeFormat("en", { dateStyle: "long", timeZone: "UTC" }).format(new Date());

function Block({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="border-t border-border pt-8">
      <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
      <p className="mt-3 leading-relaxed text-muted-foreground">{children}</p>
    </section>
  );
}

export default function PrivacyPage() {
  const email = contactEmail();
  const mail = (
    <a href={`mailto:${email}`} className="text-foreground underline underline-offset-4 transition-colors hover:text-brand">
      {email}
    </a>
  );

  return (
    <PageShell>
      <h1 className="text-5xl font-semibold tracking-[-0.04em]">Privacy</h1>
      <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
        This page explains what we collect when you join the Sparkbase waitlist or newsletter.
      </p>

      <div className="mt-14 space-y-10">
        <Block title="What we collect">
          Your email address, which list you joined (waitlist or newsletter), and where you came from if your link
          included a source. To limit abuse we also store a hashed version of your IP address, never the address
          itself.
        </Block>
        <Block title="How we use it">
          We use your email only to contact you about Sparkbase: early access for the waitlist, and updates for the
          newsletter.
        </Block>
        <Block title="Sharing">We don&apos;t sell your email address.</Block>
        <Block title="Unsubscribing">
          Every email we send includes an unsubscribe link. Using it removes you from that list.
        </Block>
        <Block title="Deleting your data">To have your data deleted, email {mail}.</Block>
        {PLAUSIBLE_DOMAIN && (
          <Block title="Analytics">
            If analytics are enabled, we use Plausible, which doesn&apos;t use cookies and doesn&apos;t track you across
            sites.
          </Block>
        )}
        <Block title="Contact">{mail}</Block>
      </div>

      <p className="mt-14 font-mono text-xs text-muted-foreground">Last updated: {LAST_UPDATED}</p>
    </PageShell>
  );
}
