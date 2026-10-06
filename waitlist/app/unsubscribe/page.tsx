import type { Metadata } from "next";
import { HomeLink, PageShell } from "@/components/page-shell";
import { Button } from "@/components/ui/button";
import { contactEmail } from "@/lib/site";
import { getTokenStatus, type TokenStatus } from "@/lib/subscribers";
import { isUuid } from "@/lib/uuid";
import { confirmUnsubscribe } from "./actions";

export const metadata: Metadata = {
  title: "Unsubscribe",
  robots: { index: false, follow: false },
};

// Opening the link never changes data: mail scanners and link previews prefetch URLs.
// The row is only updated when the person presses the button (a POST server action).
export default async function UnsubscribePage({ searchParams }: PageProps<"/unsubscribe">) {
  const { token } = await searchParams;
  let status: TokenStatus = "unknown";
  if (typeof token === "string" && isUuid(token)) {
    status = await getTokenStatus(token);
  }

  if (status === "unsubscribed") {
    return (
      <PageShell>
        <h1 className="text-5xl font-semibold tracking-[-0.04em]">You&apos;re unsubscribed.</h1>
        <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
          You won&apos;t get any more emails from Sparkbase on this list.
        </p>
        <HomeLink className="mt-10" />
      </PageShell>
    );
  }

  if (status === "active") {
    return (
      <PageShell>
        {/* TODO(copy): confirm step was approved but has no copy in the deck yet. */}
        <h1 className="text-5xl font-semibold tracking-[-0.04em]">Unsubscribe</h1>
        <form action={confirmUnsubscribe} className="mt-10">
          <input type="hidden" name="token" value={token} />
          <Button type="submit" size="pill">
            Unsubscribe
          </Button>
        </form>
      </PageShell>
    );
  }

  const email = contactEmail();
  return (
    <PageShell>
      <h1 className="text-5xl font-semibold tracking-[-0.04em]">This link isn&apos;t valid.</h1>
      <p className="mt-5 text-lg leading-relaxed text-muted-foreground">
        If you keep getting emails you don&apos;t want, contact us at{" "}
        <a href={`mailto:${email}`} className="text-foreground underline underline-offset-4 transition-colors hover:text-brand">
          {email}
        </a>
        .
      </p>
      <HomeLink className="mt-10" />
    </PageShell>
  );
}
