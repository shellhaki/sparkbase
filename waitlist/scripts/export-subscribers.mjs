// Prints active subscribers of one list as CSV to stdout.
// Usage: npm run export -- --list waitlist > waitlist.csv

import { parseArgs } from "node:util";
import postgres from "postgres";

const LISTS = ["waitlist", "newsletter"];

const { values } = parseArgs({ options: { list: { type: "string" } } });
if (!LISTS.includes(values.list)) {
  console.error("Usage: npm run export -- --list <waitlist|newsletter>");
  process.exit(1);
}

const url = process.env.DATABASE_URL;
if (!url) {
  console.error("DATABASE_URL is not set.");
  process.exit(1);
}

const sql = postgres(url, { max: 1 });

function csvField(value) {
  if (value === null || value === undefined) return "";
  const text = value instanceof Date ? value.toISOString() : String(value);
  // Quote every field and neutralise spreadsheet formula injection.
  const safe = /^[=+\-@\t\r]/.test(text) ? `'${text}` : text;
  return `"${safe.replaceAll('"', '""')}"`;
}

try {
  const rows = await sql`
    select email, list, source, unsubscribe_token, created_at
    from subscribers
    where list = ${values.list}
      and unsubscribed_at is null
    order by created_at
  `;

  const columns = ["email", "list", "source", "unsubscribe_token", "created_at"];
  process.stdout.write(columns.join(",") + "\n");
  for (const row of rows) {
    process.stdout.write(columns.map((c) => csvField(row[c])).join(",") + "\n");
  }
  console.error(`Exported ${rows.length} rows from ${values.list}.`);
} catch (error) {
  console.error("Export failed:", error.message);
  process.exitCode = 1;
} finally {
  await sql.end();
}
