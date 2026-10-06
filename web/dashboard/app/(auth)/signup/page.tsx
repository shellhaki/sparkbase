"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { AuthCard } from "@/components/auth-card";
import { Button, ErrorText, Field, Input } from "@/components/ui";
import { api } from "@/lib/api";

export default function SignupPage() {
  const router = useRouter();
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });

  return (
    <AuthCard
      title="Create an account"
      footer={
        <>
          Have an account?{" "}
          <Link href="/login" className="text-foreground underline underline-offset-4">
            Sign in
          </Link>
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
            await api("POST", "/v1/auth/signup", form);
            router.replace("/");
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Name">
          <Input autoComplete="name" value={form.name} onChange={set("name")} autoFocus />
        </Field>
        <Field label="Email">
          <Input type="email" autoComplete="email" required value={form.email} onChange={set("email")} />
        </Field>
        <Field label="Password" hint="At least 8 characters.">
          <Input type="password" autoComplete="new-password" required minLength={8} value={form.password} onChange={set("password")} />
        </Field>
        <ErrorText error={error} />
        <Button type="submit" className="w-full" loading={busy}>
          Create account
        </Button>
      </form>
    </AuthCard>
  );
}
