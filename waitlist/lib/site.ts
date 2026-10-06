export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "http://localhost:3000").replace(/\/+$/, "");
export const GITHUB_URL = process.env.NEXT_PUBLIC_GITHUB_URL || "https://github.com/shellhaki";
export const X_URL = process.env.NEXT_PUBLIC_X_URL || "https://x.com/haki_xer";
export const PLAUSIBLE_DOMAIN = process.env.NEXT_PUBLIC_PLAUSIBLE_DOMAIN || "";

export const SITE_TITLE = "Sparkbase: The data layer for your backend";
export const SITE_DESCRIPTION =
  "Self-hosted data services for your backend. Postgres, Redis and object storage with native connection strings, an SDK and an API. Free and open source.";

/** Server-only. Fails loudly in production instead of rendering a wrong address. */
export function contactEmail(): string {
  const email = process.env.CONTACT_EMAIL;
  if (email) return email;
  if (process.env.NODE_ENV === "production") throw new Error("CONTACT_EMAIL is not set.");
  return "contact@example.com"; // TODO: dev-only placeholder, set CONTACT_EMAIL in .env.local
}
