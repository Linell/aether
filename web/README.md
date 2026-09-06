# aether-web

Landing page for [aether](https://github.com/linell/aether): personal daemons with memory, schedules, and one thread you can reach from anywhere.

Static [Astro](https://astro.build) site, managed with pnpm. Pushes to `main` that touch `web/` deploy it to [linell.github.io/aether](https://linell.github.io/aether/) through `.github/workflows/pages.yml`.

```
pnpm install
pnpm dev     # local dev server
pnpm check   # type-check .astro and .ts
pnpm build   # static output in dist/
```

## Layout

- `src/pages/index.astro` — the page; section copy lives at the top of the file.
- `src/components/Smoke.astro` — canvas mount with a CSS gradient fallback.
- `src/lib/smoke/` — the WebGL2 smoke shader and its TypeScript renderer.
- `src/styles/global.css` — tokens, type, reveal transitions.
