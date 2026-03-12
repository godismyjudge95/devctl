---
name: vue-frontend
description: Patterns and conventions for working on the devctl Vue 3 SPA — Pinia stores, API wrappers, shadcn-vue components, Tailwind v4, and the Vite/go:embed pipeline
license: MIT
compatibility: opencode
metadata:
  layer: frontend
  language: typescript
---

## Overview

The frontend is a Vue 3 + TypeScript SPA in `frontend/`. The Vite build outputs to `ui/dist/`, which is embedded into the Go binary at compile time via `//go:embed ui/dist`. During dev, Vite proxies `/api` and `/ws` to the Go server on `:4000`.

## Key source locations

| Path | Purpose |
|---|---|
| `frontend/src/App.vue` | App shell — sidebar nav, dark mode, SSE/WebSocket init on mount |
| `frontend/src/router/index.ts` | vue-router routes (`/services`, `/sites`, `/php`, `/dumps`, `/mail`, `/settings`) |
| `frontend/src/views/` | One `.vue` file per route |
| `frontend/src/stores/` | Pinia stores (services, sites, dumps, settings) |
| `frontend/src/lib/api.ts` | All typed fetch wrappers — **always add new API calls here** |
| `frontend/src/components/ui/` | shadcn-vue primitives — use these, don't write raw HTML controls |
| `frontend/src/composables/` | Small reusable composables (e.g. `useDarkMode`) |

## Adding a new API call

All REST calls go through `frontend/src/lib/api.ts`. The internal `request<T>` helper handles fetch, JSON, and errors uniformly:

```ts
// Add a typed wrapper:
export const getExample = (id: string) =>
  request<Example>('GET', `/api/example/${id}`)

export const createExample = (body: CreateExampleInput) =>
  request<Example>('POST', '/api/example', body)
```

Errors thrown by `request()` are plain `Error` objects — catch them in the store or handler and show a toast via `vue-sonner`.

## Pinia store pattern

Follow the pattern in existing stores (`stores/services.ts`, `stores/sites.ts`):

```ts
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getExample } from '@/lib/api'

export const useExampleStore = defineStore('example', () => {
  const items = ref<Example[]>([])
  const loading = ref(false)

  async function fetchAll() {
    loading.value = true
    try {
      items.value = await getExample()
    } finally {
      loading.value = false
    }
  }

  return { items, loading, fetchAll }
})
```

- Use composition API (`setup()` style) in stores — not options API.
- Stores are the only place that calls `api.ts` functions.
- Views call store actions; they don't call `api.ts` directly.

## Adding a new view

1. Create `frontend/src/views/ExampleView.vue`.
2. Add a route in `frontend/src/router/index.ts`.
3. Add a sidebar entry in `App.vue` (icon from `lucide-vue-next`, label, path).

## UI components

Use **shadcn-vue** components from `frontend/src/components/ui/`. Available:
`Badge`, `Button`, `Card` (+ `CardHeader`/`CardContent`/`CardFooter`), `Dialog`, `Sheet`, `Select`, `Table`, `Sonner`, `Separator`, `Input`, `Label`, `Switch`, `Tooltip`, and more.

Don't install new UI libraries — compose from existing primitives.

## Tailwind v4

The project uses **Tailwind CSS v4** (CSS-first config, no `tailwind.config.js`). Configuration is in `frontend/src/style.css`. Use standard utility classes; the dark mode variant is `dark:`.

Dark mode is toggled via the `useDarkMode` composable which persists to `localStorage` and toggles a `dark` class on `<html>`.

## SSE (service status stream)

The services store opens an `EventSource` to `/api/services/events` in `App.vue` on mount. To consume it in a new store, follow `stores/services.ts` — listen to the shared `EventSource` reference, parse `JSON.parse(event.data)`.

## WebSocket (dumps)

The dumps WebSocket (`/ws/dumps`) is also opened once in `App.vue` and shared via `stores/dumps.ts`. New real-time features should follow this hub-and-store pattern rather than opening additional WebSocket connections.

## Dev / build commands

```sh
# From repo root:
make dev-ui       # Vite HMR dev server (proxies /api + /ws to :4000)
make build-ui     # Production Vite build → ui/dist/
make build        # build-ui + go build

# From frontend/:
npm run dev       # same as make dev-ui
npm run build     # production build
npm run type-check  # vue-tsc type checking
```

## go:embed pipeline

`main.go` embeds `ui/dist` with `//go:embed ui/dist`. The `ui/dist/` directory is git-ignored. Always run `make build-ui` (or `make build`) before building the binary with a working UI.
