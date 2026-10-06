"use client";

import { useEffect, useId, useRef, useState, useSyncExternalStore, type FormEvent } from "react";
import { ArrowRightIcon, CheckIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { isList, normalizeEmail, SOURCE_MAX_LENGTH, type List } from "@/lib/lists";
import { cn } from "@/lib/utils";

declare global {
  interface Window {
    plausible?: (event: string, options?: { props?: Record<string, string> }) => void;
  }
}

const COPY: Record<List, { label: string; button: string; loading: string; helper: string; success: string; switchTo: string }> = {
  waitlist: {
    label: "Release list",
    button: "Notify me",
    loading: "Joining...",
    helper: "One email when the first release ships. No spam.",
    success: "You're on the list. We'll email you when the first release ships.",
    switchTo: "Also want the newsletter?",
  },
  newsletter: {
    label: "Newsletter",
    button: "Subscribe",
    loading: "Subscribing...",
    helper: "Build-in-public updates as we ship. Unsubscribe any time.",
    success: "You're subscribed. Thanks for following along.",
    switchTo: "Also join the release list?",
  },
};

const ERRORS = {
  invalid: "Enter a valid email address.",
  rateLimited: "Too many attempts. Try again in a minute.",
  server: "Something went wrong. Please try again.",
  network: "Can't reach the server. Check your connection and try again.",
};

type Status = "idle" | "loading" | "success";

// ?mode=newsletter preselects the newsletter. Read through an external store so the
// server render (always "waitlist") and the client hydrate without a mismatch.
function subscribeToUrl(onChange: () => void) {
  window.addEventListener("popstate", onChange);
  return () => window.removeEventListener("popstate", onChange);
}
function urlMode(): List {
  return new URLSearchParams(window.location.search).get("mode") === "newsletter" ? "newsletter" : "waitlist";
}

/** utm_source, else the referring host when it isn't this site. */
function captureSource(): string | undefined {
  const utm = new URLSearchParams(window.location.search).get("utm_source")?.trim();
  if (utm) return utm.slice(0, SOURCE_MAX_LENGTH);
  try {
    const ref = document.referrer ? new URL(document.referrer) : null;
    if (ref && ref.host !== window.location.host) return ref.host.slice(0, SOURCE_MAX_LENGTH);
  } catch {}
  return undefined;
}

export function SignupForm({ id, align = "center" }: { id?: string; align?: "center" | "start" }) {
  const uid = useId();
  const initialMode = useSyncExternalStore(subscribeToUrl, urlMode, () => "waitlist" as const);
  const [chosenMode, setChosenMode] = useState<List | null>(null);
  const mode = chosenMode ?? initialMode;

  const [status, setStatus] = useState<Status>("idle");
  const [error, setError] = useState("");
  const [email, setEmail] = useState("");
  const [doneMode, setDoneMode] = useState<List>("waitlist");

  const emailRef = useRef<HTMLInputElement>(null);
  const focusEmailOnOpen = useRef(false);

  // The nav button links to "#join". When it targets this form, select the release list and focus the field.
  useEffect(() => {
    if (!id) return;
    function onClick(event: MouseEvent) {
      const link = (event.target as Element | null)?.closest?.(`a[href$="#${id}"]`);
      if (!link || window.location.pathname !== "/") return;
      setChosenMode("waitlist");
      setStatus("idle");
      setError("");
      // Wait for smooth scrolling to start, then focus without jumping.
      window.setTimeout(() => emailRef.current?.focus({ preventScroll: true }), 300);
    }
    document.addEventListener("click", onClick);
    return () => document.removeEventListener("click", onClick);
  }, [id]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (status === "loading") return;

    const normalized = normalizeEmail(email);
    if (!normalized) {
      setError(ERRORS.invalid);
      emailRef.current?.focus();
      return;
    }

    const website = new FormData(event.currentTarget).get("website");
    setError("");
    setStatus("loading");

    let response: Response;
    try {
      response = await fetch("/api/subscribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: normalized, list: mode, source: captureSource(), website }),
      });
    } catch {
      setStatus("idle");
      setError(ERRORS.network);
      return;
    }

    if (response.ok) {
      setDoneMode(mode);
      setStatus("success");
      setEmail("");
      window.plausible?.("signup", { props: { list: mode } });
      return;
    }

    setStatus("idle");
    setError(
      response.status === 422 ? ERRORS.invalid : response.status === 429 ? ERRORS.rateLimited : ERRORS.server,
    );
    if (response.status === 422) emailRef.current?.focus();
  }

  function reopenWith(next: List) {
    focusEmailOnOpen.current = true;
    setChosenMode(next);
    setStatus("idle");
    setError("");
  }

  const copy = COPY[mode];
  const errorId = `${uid}-error`;
  const helperId = `${uid}-helper`;
  const centered = align === "center";

  return (
    <div id={id} className={cn("w-full max-w-md", centered && "mx-auto")}>
      <div aria-live="polite" role="status">
        {status === "success" && (
          <div className={cn("space-y-3", centered && "text-center")}>
            <p
              tabIndex={-1}
              ref={(el) => el?.focus()}
              className="flex items-center gap-3 rounded-2xl border bg-card px-5 py-4 text-left text-[15px] leading-relaxed outline-none"
            >
              <CheckIcon aria-hidden="true" className="size-4 shrink-0 text-brand" />
              {COPY[doneMode].success}
            </p>
            <Button
              type="button"
              variant="link"
              onClick={() => reopenWith(doneMode === "waitlist" ? "newsletter" : "waitlist")}
              className="text-muted-foreground hover:text-brand"
            >
              {COPY[doneMode].switchTo}
            </Button>
          </div>
        )}
      </div>

      {status !== "success" && (
        <form noValidate onSubmit={onSubmit} className="space-y-3">
          <div className={cn("flex", centered && "justify-center")}>
            <ToggleGroup
              type="single"
              aria-label="Choose a list"
              value={mode}
              onValueChange={(value) => {
                if (!isList(value)) return; // ignore deselect: one list is always chosen
                setChosenMode(value);
                setError("");
              }}
              spacing={0}
              className="rounded-full border bg-card p-1"
            >
              {(["waitlist", "newsletter"] as const).map((option) => (
                <ToggleGroupItem
                  key={option}
                  value={option}
                  className="h-8 rounded-full! px-4 text-sm text-muted-foreground hover:bg-transparent hover:text-foreground data-[state=on]:bg-foreground data-[state=on]:font-medium data-[state=on]:text-background"
                >
                  {COPY[option].label}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>

          <div className="flex flex-col gap-2 sm:flex-row">
            <label htmlFor={`${uid}-email`} className="sr-only">
              Email address
            </label>
            <Input
              ref={(el) => {
                emailRef.current = el;
                if (el && focusEmailOnOpen.current) {
                  focusEmailOnOpen.current = false;
                  el.focus();
                }
              }}
              id={`${uid}-email`}
              type="email"
              name="email"
              autoComplete="email"
              inputMode="email"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              aria-invalid={error === ERRORS.invalid || undefined}
              aria-describedby={`${error ? errorId : ""} ${helperId}`.trim()}
              className="h-12 w-full min-w-0 rounded-full bg-card px-5 text-[15px] md:text-[15px] sm:flex-1 dark:bg-card"
            />
            <Button type="submit" size="pill" disabled={status === "loading"} className="disabled:opacity-80">
              {status === "loading" ? copy.loading : copy.button}
              {status !== "loading" && <ArrowRightIcon aria-hidden="true" data-icon="inline-end" />}
            </Button>
          </div>

          {/* Honeypot. Hidden from people and assistive tech; bots tend to fill every field. */}
          <div aria-hidden="true" className="absolute -left-[9999px] h-px w-px overflow-hidden">
            <label htmlFor={`${uid}-website`}>Website</label>
            <input id={`${uid}-website`} type="text" name="website" tabIndex={-1} autoComplete="off" defaultValue="" />
          </div>

          <p id={errorId} aria-live="polite" className={cn("text-sm text-destructive", centered && "text-center")}>
            {error}
          </p>
          <p id={helperId} className={cn("text-sm text-muted-foreground", centered && "text-center")}>
            {copy.helper}
          </p>
        </form>
      )}
    </div>
  );
}
