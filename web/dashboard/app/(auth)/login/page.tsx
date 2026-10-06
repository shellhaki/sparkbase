"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AuthCard } from "@/components/auth-card";
import { Button, ErrorText, Field, Input } from "@/components/ui";
import { api } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    api("GET", "/v1/setup")
      .then((s) => {
        if (s.open) router.replace("/setup");
      })
      .catch(() => {});
    api("GET", "/v1/me")
      .then(() => router.replace("/"))
      .catch(() => {});
  }, [router]);

  return (
    <AuthCard
      title="Sign in"
      description="Use the email and password for this Sparkbase install."
      footer="New here? Ask your admin for an invite."
    >
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          try {
            await api("POST", "/v1/auth/login", { email, password });
            router.replace("/");
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Email">
          <Input type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoFocus />
        </Field>
        <Field label="Password">
          <Input type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <ErrorText error={error} />
        <Button type="submit" className="w-full" loading={busy}>
          Sign in
        </Button>
        <Link href="/reset-password" className="block text-center text-sm text-muted-foreground hover:text-foreground">
          Forgot your password?
        </Link>
      </form>
    </AuthCard>
  );
}
