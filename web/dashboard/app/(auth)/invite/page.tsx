"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { AuthCard } from "@/components/auth-card";
import { Button, ErrorText, Field, Input, Spinner } from "@/components/ui";
import { api } from "@/lib/api";

function InviteForm() {
  const router = useRouter();
  const token = useSearchParams().get("token") ?? "";
  const [invite, setInvite] = useState<{ email: string; role: string } | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!token) {
      setLoadError(new Error("This invite link is missing its token."));
      return;
    }
    api("GET", `/v1/auth/invites/${encodeURIComponent(token)}`)
      .then(setInvite)
      .catch(() => setLoadError(new Error("This invite is invalid, used or expired. Ask your admin for a new one.")));
  }, [token]);

  if (loadError)
    return (
      <AuthCard title="Invite not valid">
        <ErrorText error={loadError} />
      </AuthCard>
    );
  if (!invite)
    return (
      <AuthCard title="Accept invite">
        <Spinner />
      </AuthCard>
    );

  return (
    <AuthCard
      title="Join Sparkbase"
      description={
        <>
          You were invited as <strong className="text-foreground">{invite.role}</strong> with <strong className="text-foreground">{invite.email}</strong>.
        </>
      }
    >
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          try {
            await api("POST", "/v1/auth/invites/accept", { token, name, password });
            router.replace("/");
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Your name">
          <Input autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Choose a password" hint="At least 8 characters.">
          <Input type="password" autoComplete="new-password" required minLength={8} value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <ErrorText error={error} />
        <Button type="submit" className="w-full" loading={busy}>
          Create account
        </Button>
      </form>
    </AuthCard>
  );
}

export default function InvitePage() {
  return (
    <Suspense>
      <InviteForm />
    </Suspense>
  );
}
