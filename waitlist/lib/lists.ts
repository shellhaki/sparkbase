export const LISTS = ["waitlist", "newsletter"] as const;
export type List = (typeof LISTS)[number];

export function isList(value: unknown): value is List {
  return typeof value === "string" && (LISTS as readonly string[]).includes(value);
}

export const EMAIL_MAX_LENGTH = 254;
export const SOURCE_MAX_LENGTH = 100;

// Deliberately simple: one "@", no spaces, a dot in the domain.
const EMAIL_PATTERN = /^[^\s@]{1,64}@[^\s@]+\.[^\s@]{2,}$/;

export function normalizeEmail(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const email = value.trim().toLowerCase();
  if (email.length > EMAIL_MAX_LENGTH || !EMAIL_PATTERN.test(email)) return null;
  return email;
}
