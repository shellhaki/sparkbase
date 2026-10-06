"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AuthCard } from "@/components/auth-card";
import { Button, ErrorText, Field, Input } from "@/components/ui";
import { api } from "@/lib/api";

export default function SetupPage() {
  const router = useRouter();
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    api("GET", "/v1/setup")
      .then((s) => !s.open && router.replace("/login"))
      .catch((e) => setError(e));
  }, [router]);

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => setForm({ ...form, [k]: e.target.value });

  return (
    <AuthCard
      title="Set up Sparkbase"
      description="The first account on this install is the admin. Everyone else joins by invite."
    >
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          try {
            await api("POST", "/v1/setup", form);
            router.replace("/");
          } catch (err) {
            setError(err);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Your name">
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
          Create admin account
        </Button>
      </form>
    </AuthCard>
  );
}
