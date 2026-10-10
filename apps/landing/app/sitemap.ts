import type { MetadataRoute } from "next";
import { allSlugs } from "@/lib/docs";
import { SITE_URL } from "@/lib/site";

export default function sitemap(): MetadataRoute.Sitemap {
  return [{ url: `${SITE_URL}/`, priority: 1 }, ...allSlugs().map((s) => ({ url: `${SITE_URL}/docs/${s}`, priority: 0.6 }))];
}
