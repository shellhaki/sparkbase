"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { cn } from "@/lib/utils";

type Currency = "USD" | "NGN";

const PLANS = [
  { name: "Free", usd: 0 },
  { name: "Pro", usd: 7, featured: true },
  { name: "Max", usd: 15 },
];

// TODO: fixed rate set at build time. Update NEXT_PUBLIC_USD_TO_NGN when the rate moves.
const USD_TO_NGN = Number(process.env.NEXT_PUBLIC_USD_TO_NGN) || 1500;

function formatPrice(usd: number, currency: Currency) {
  if (currency === "USD") return `$${usd}`;
  // Round to the nearest ₦100 so prices read cleanly.
  const ngn = Math.round((usd * USD_TO_NGN) / 100) * 100;
  return `₦${ngn.toLocaleString("en-NG")}`;
}

export function Pricing() {
  const [currency, setCurrency] = useState<Currency>("USD");

  return (
    <div>
      <div className="flex justify-center">
        <ToggleGroup
          type="single"
          aria-label="Currency"
          value={currency}
          onValueChange={(value) => {
            if (value === "USD" || value === "NGN") setCurrency(value);
          }}
          spacing={0}
          className="rounded-full border bg-card p-1"
        >
          {(["USD", "NGN"] as const).map((option) => (
            <ToggleGroupItem
              key={option}
              value={option}
              className="h-8 rounded-full! px-4 font-mono text-xs text-muted-foreground hover:bg-transparent hover:text-foreground data-[state=on]:bg-foreground data-[state=on]:text-background"
            >
              {option}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <ul className="mt-10 grid gap-4 md:grid-cols-3">
        {PLANS.map((plan) => (
          <li key={plan.name}>
            <Card
              className={cn(
                "h-full gap-0 rounded-3xl border bg-card py-0 ring-0",
                plan.featured && "border-brand/60 shadow-[0_0_0_1px_var(--brand)]",
              )}
            >
              <CardHeader className="px-8 pt-8">
                <CardTitle className="text-base font-medium text-muted-foreground">{plan.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-1 flex-col px-8 pt-6 pb-8">
                <p className="flex items-baseline gap-2">
                  <span className="text-5xl font-semibold tracking-[-0.04em] tabular-nums">
                    {formatPrice(plan.usd, currency)}
                  </span>
                  <span className="text-muted-foreground">/ resource / month</span>
                </p>
                <Button
                  asChild
                  size="pill"
                  variant={plan.featured ? "default" : "secondary"}
                  className="mt-8 w-full"
                >
                  <a href="#join">Join the waitlist</a>
                </Button>
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
    </div>
  );
}
