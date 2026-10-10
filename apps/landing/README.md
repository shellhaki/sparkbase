# Sparkbase landing page

The public site: the landing page at `/` and the documentation at `/docs`, rendered from the repository's `docs/` folder at build time. No database, no forms, no environment variables required.

## Run locally

```bash
cd apps/landing
bun install
bun dev        # http://localhost:3100
```

## Deploy on Vercel

1. Import `github.com/shellhaki/sparkbase` in Vercel.
2. Set **Root Directory** to `apps/landing`. Keep "Include files outside the root directory" on (the default), because the docs are read from `../../docs`.
3. Framework preset: Next.js. Vercel picks up `bun.lock` and installs with Bun.
4. Optional: set `NEXT_PUBLIC_SITE_URL` to your domain (for example `https://sparkbase.dev`) so Open Graph images and the sitemap use absolute URLs.
5. Deploy. Every push to `main` redeploys; changes under `docs/` update the docs pages.

## Screenshots

`public/shots/*.png` are real screenshots of the dashboard at 2x. Retake them after UI changes so the page stays honest.
