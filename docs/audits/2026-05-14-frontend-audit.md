# Frontend Audit — 2026-05-14

## Summary

Read-only sweep of `apps/web` (Next.js 16.2.2 + React 19 + Tailwind v4 + shadcn). Scope: every route under `app/**`, every component under `components/**`, every SDK file under `lib/**`.

The recent `5029759` fix (`NEXT_PUBLIC_API_URL` vs `API_URL_INTERNAL`) has held — every server-side SDK and page reads `API_URL_INTERNAL ?? NEXT_PUBLIC_API_URL`, every client SDK reads `NEXT_PUBLIC_API_URL`. No regression there.

The console renders, but four classes of problem dominate this report: (1) the entire console is non-responsive below `md` — `ml-60` is unconditional and the 240 px sidebar eats 60 % of an iPhone-SE viewport, (2) zero `error.tsx` and zero `loading.tsx` boundaries anywhere, so any backend failure on a server-component page hands the user Next's default error screen, (3) two marketing SVG components (`SystemVector`, `ApprovalVector`) reference CSS variables that don't exist (`--text-muted`, `--border-hover`, `--text-primary` were renamed to `--color-*` under the `@theme inline` rewrite), and (4) the landing-hero "Skip to dashboard" CTA points at `/console/dashboard`, a route that does not exist (it's `/console`). Stand-alone lint also has 10 errors + 2 warnings from React 19's stricter compiler.

**Severity counts:** Critical 4 · High 9 · Medium 11 · Low 8.

## Critical (broken pages / crash / data leak)

### F-1: Hero "Skip to dashboard" CTA hits a non-existent route

**Where:** `components/sections/Hero.tsx:107`
**Repro:** Sign in (so `nexis_session` is set), visit `/`, scroll to the hero. The pill "Skip to dashboard" bottom-right links to `/console/dashboard`. That route does not exist — the dashboard page is `/console` (file `app/(app)/console/page.tsx`). Click hands the user a 404.
**Fix:** Change `href={"/console/dashboard" as Route}` to `href={"/console" as Route}`. Drop the `as Route` cast — `/console` is a real typed route now.

### F-2: Console layout is unusable on mobile

**Where:** `app/(app)/console/layout.tsx:98` (`<div className="ml-60">`) and `components/console/Sidebar.tsx:577–582` (`fixed inset-y-0 left-0 w-60`)
**Repro:** Open any `/console/*` route at ≤640 px. The sidebar is fixed-positioned at 240 px and `<main>` has an unconditional `ml-60` (60 % of an iPhone-SE viewport). There is no hamburger toggle, no `md:` breakpoint, no Sheet. Every console page is unusable on a phone.
**Fix:** Make the sidebar `hidden md:flex`, render a Sheet (Radix Dialog) trigger in the Topbar for mobile, and switch the gutter to `md:ml-60`. Same pattern as the public Navbar.

### F-3: No `error.tsx` boundary anywhere — backend errors crash entire pages

**Where:** `app` tree (no `app/**/error.tsx` exists)
**Repro:** `grep -r "error.tsx" app/` → empty. Take any console page (e.g. `/console/cost`, `/console/eval`) and stop the control-plane. The server-component `fetch()` rejects, the `await r.json()` in nested handlers throws, and the user sees Next's stock framework error. No "Something went wrong / try again" UX, no telemetry hook.
**Fix:** Add `app/(app)/console/error.tsx` (route-segment error boundary, client component) and `app/error.tsx` (root). Render a branded "Something went wrong · Reload · Contact support" UI with `reset()` from Next.

### F-4: No `loading.tsx` boundary anywhere — every server fetch shows blank chrome

**Where:** `app` tree (no `app/**/loading.tsx` exists)
**Repro:** Throttle the control-plane to 3 s. Visit `/console/incidents`. The entire page is blank (no chrome, no skeleton) until the server `fetch` resolves and the streamed HTML arrives. The console layout's auth check `await fetch(/v1/me)` also blocks.
**Fix:** Add `app/(app)/console/loading.tsx` that renders the Topbar/Sidebar skeleton + a content skeleton (use `bg-[var(--color-muted)] animate-pulse`). Likewise `app/(app)/console/incidents/loading.tsx` for the table skeleton.

## High

### F-5: `SystemVector` SVG references CSS vars that don't exist (light + dark broken)

**Where:** `components/ui/SystemVector.tsx:110, 119, 158, 161, 164, 167, 170, 176, 179, 182, 185`
**Repro:** The hero on `/` (right column on desktop) renders `<SystemVector>`. The component uses `stroke="var(--border-hover)"` and `fill="var(--text-muted)"` / `var(--text-primary)`. Those tokens only exist as `--color-border-hover`, `--color-text-muted`, `--color-text-primary` inside `@theme inline` in `globals.css`. Tailwind v4's `@theme inline` does NOT emit `:root` custom properties — the vars are inlined into the generated utilities. Raw `var(--text-muted)` therefore resolves to the SVG fallback (black) — labels are invisible in dark mode and partially-broken in light. This is exactly what the user described as "landing looked bad in light mode" for the hero right column.
**Fix:** Replace every `var(--text-muted)` → `var(--muted-foreground)`, `var(--text-primary)` → `var(--foreground)`, `var(--border-hover)` → `var(--accent)`. These three are actual `:root` properties defined in `globals.css`.

### F-6: `ApprovalVector` SVG has the same broken vars

**Where:** `components/ui/ApprovalVector.tsx:14, 17, 20, 23, 32, 35, 38, 41, 44`
**Repro:** Used in marketing surfaces — same root cause as F-5. SVG `<text>` elements render with `fill="var(--text-muted)"` / `var(--text-primary)`, both undefined; falls back to black. Labels invisible in dark mode.
**Fix:** Same substitution as F-5.

### F-7: 10 pre-existing lint errors — React 19 / @react-hooks compiler

**Where:** see lint census below
**Repro:** `./node_modules/.bin/eslint .` → 10 errors, 2 warnings (confirmed).
**Fix:** For each `react-hooks/set-state-in-effect` error (7 of them), refactor to either `useSyncExternalStore`, a `useState(initializer)`, or a `useReducer`. The `Date.now()` + `bannerStartedAtRef` pair in `incidents/[id]/client.tsx:242, 456` violates React 19 purity rules — initialize the ref with `useState`-style lazy initializer or a `useRef<number | null>(null)` then set on mount. Two `no-explicit-any` errors in `tests/waitlist.test.ts` — replace with `unknown` or proper types.

### F-8: `/console/dashboard` referenced in changelog copy

**Where:** `app/changelog/page.tsx:134`
**Repro:** "BREAKING: removed deprecated /dashboard route — use /console/dashboard." Public-facing copy promising a path that does not exist. Compounds with F-1.
**Fix:** Update copy to "use /console" or rename to "/console (formerly /dashboard)".

### F-9: Sign-in/sign-up redirect to `/dashboard`, which redirects to `/console` — double hop

**Where:** `app/(auth)/sign-in/page.tsx:152, 155` (`/dashboard` default); `app/(auth)/sign-up/page.tsx:203` (`router.push("/dashboard")`); `app/(app)/dashboard/page.tsx:7` (redirect to `/console`)
**Repro:** Submit sign-up → `router.push("/dashboard")` → server renders `redirect("/console")` → `/console` finally renders. Two RTTs. Visible 100–400 ms flash of nothing.
**Fix:** Change every `/dashboard` push to `/console` directly. Keep `app/(app)/dashboard/page.tsx` as a redirect for old bookmarks.

### F-10: Comparison table mobile header doesn't follow row layout

**Where:** `components/sections/Comparison.tsx:69`
**Repro:** Header row hardcoded `grid-cols-[1.2fr_1fr_1fr]` always; data rows use `grid-cols-1 md:grid-cols-[1.2fr_1fr_1fr]`. On <768 px the header stays in 3 narrow columns while the body stacks vertically — header collapses to "Dim... With... Wi..." truncated.
**Fix:** Same `grid-cols-1 md:grid-cols-[1.2fr_1fr_1fr]` on the header, and hide the "Without"/"With" header labels on small screens (`hidden md:block`).

### F-11: Pricing matrix has 4-col grid with no mobile fallback

**Where:** `app/pricing/page.tsx:203, 220`
**Repro:** `grid-cols-[1.4fr_1fr_1fr_1fr]` is unconditional. Below `md` the four columns become unreadable narrow strips.
**Fix:** Wrap each header cell + each row in `hidden md:block` for the per-tier cells; on mobile render a stacked card list instead. Or wrap the whole matrix in `overflow-x-auto`.

### F-12: 18 console tables, only 1 has `overflow-x-auto`

**Where:** `app/(app)/console/recovery/client.tsx:329` (8-col), `workflows/client.tsx` (8-col, 213 `colSpan={8}`), `cost/client.tsx:338`, `validator/client.tsx:315`, `projects/[id]/client.tsx:289`, plus settings tables (api-keys, workspaces, members)
**Repro:** Visit any of those at 375 px. The `<table className="w-full text-sm">` overflows the viewport silently — cells get clipped by the surrounding `rounded-lg overflow-hidden` wrapper.
**Fix:** Wrap each table in `<div className="overflow-x-auto">…</div>`. Replace the outer `overflow-hidden` with `overflow-clip` or remove it.

### F-13: Integration count drifts between three sources

**Where:** `app/integrations/page.tsx` (30 entries, metadata says 28), `components/sections/IntegrationsTeaser.tsx` (28 logos rendered), `app/status/page.tsx:68` ("28 integration probes")
**Repro:** Count `id: "…"` entries in `app/integrations/page.tsx` → 30 (added `launchdarkly` + `vault`). But the metadata, the landing teaser, and the status page hero all hardcode "28".
**Fix:** Either drop the two extras to keep "28", or update the three copy sites. Derive the count from `PROVIDERS.length` so this never drifts again.

## Medium

### F-14: 43 hardcoded `text-zinc-*` / `bg-zinc-*` / `bg-gray-*` classes break dark mode contrast

**Where:** 23 files, see grep results. Examples:
- `components/projects/EnvironmentChip.tsx:19`
- `components/console/OperationalSegments.tsx:68, 69, 80, 82`
- `components/incidents/SeverityPill.tsx:34, 35, 37`
- `components/integrations/IntegrationCard.tsx:44` (`bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900`)
- `components/workspaces/WorkspaceSwitcher.tsx:103, 120` (`bg-gray-300`, `bg-gray-400`)
**Repro:** Each chip mixes a literal zinc/gray with the token-based theme. Light mode looks fine; dark mode the muted "queued"/"pending"/"disconnected" pills become low-contrast against the dark card. Same in the inverse — `IntegrationCard` flips `bg-zinc-900 dark:bg-zinc-100` which is the opposite of every other surface.
**Fix:** Map every zinc/gray to `var(--color-muted)`, `var(--color-muted-foreground)`, `var(--color-border)`. The `--color-*` tokens already adjust for dark.

### F-15: 20 `as never` / `as unknown as never` casts on `<Link href>`

**Where:** `components/console/Sidebar.tsx:397`, `Topbar.tsx`-adjacent, `QuickActionsRow.tsx:101, 136`, `RecoveryPipelineMini.tsx:151, 169`, `SystemStatusPanel.tsx:148, 207`, `SystemStatusPill.tsx:119, 138`, `RecentActivityFeed.tsx:80`, `ActivityTicker.tsx:96`, `NotificationsBell.tsx:185`, `DashboardKpiStrip.tsx:109`, `CommandPalette.tsx:73`, `KpiCard.tsx:164`, plus 4 in console clients.
**Repro:** Every dynamic href (`/console/incidents/${id}`, or any href from a runtime constant) is cast through `as never` to satisfy `typedRoutes`. Some are double-cast `as unknown as never` (WorkspaceSwitcher, Sidebar, CommandPalette).
**Fix:** Cast through `Route` (`import type { Route } from "next"; href={x as Route}`) — same effect, but the cast is named and intentional. Then add a `// typed-route: dynamic` comment so future readers know it's intentional.

### F-16: `WorkspaceSwitcher` skeleton uses `bg-gray-300` — invisible in dark mode

**Where:** `components/workspaces/WorkspaceSwitcher.tsx:103`
**Repro:** While the workspace list is loading, the skeleton dot is `bg-gray-300`. In dark mode that's against a near-black card and barely visible. Same issue line 120 (`bg-gray-400` when current is null).
**Fix:** Replace with `bg-[var(--color-muted-foreground)]/40` or `bg-[var(--color-border)]`.

### F-17: `LiveDemoEmbed` shows a static placeholder ("Pipeline canvas (Phase 6 wiring)") with a non-functional button

**Where:** `components/sections/LiveDemoEmbed.tsx:37, 45`
**Repro:** Landing scroll-stop: an aside that says "Pipeline canvas (Phase 6 wiring)" plus a button labelled "Run a synthetic incident" with no `onClick`. Looks like a half-built section in production.
**Fix:** Either gate behind a feature flag, or link the button to `/console/live-demo` (which is the real surface that already works), or replace the section with the actual `PipelineDiagram` from `/agents` so the "live" pitch matches the visual.

### F-18: `UserMenu.tsx` declares an unused `router`

**Where:** `components/console/UserMenu.tsx:17`
**Repro:** `const router = useRouter();` is never read — logout uses `window.location.href = "/sign-in"`. Lint warning today; will become a TS warning once `noUnusedLocals` is enabled.
**Fix:** Delete the line and the unused `useRouter` import.

### F-19: `AuditTable` lint warning: react-hooks/incompatible-library on `useReactTable`

**Where:** `components/console/AuditTable.tsx:109`
**Repro:** React Compiler refuses to memoise this component because TanStack Table 8 returns functions. Compilation skipped. Not fatal, but the page misses Compiler optimisations.
**Fix:** Wrap with `// @use-no-react-compiler` or `'use no compiler'` directive once available; otherwise accept the warning.

### F-20: `app/admin/`, `app/dashboard/`, `app/console/` are empty leftover directories

**Where:** `app/admin/`, `app/dashboard/`, `app/console/` (all empty after the Phase 3 refactor into `(app)/` and `(console)/` route groups)
**Repro:** `find app -maxdepth 1 -type d`. Three empty dirs that contribute to confusion about where pages live. Also `app/(console)/admin/`, `app/(console)/dashboard/`, `app/(console)/incidents/` are entire empty route-group dirs (with empty sub-dirs `policies/`, `teams/`, `users/`, `[id]/` …).
**Fix:** Delete all of `app/admin`, `app/dashboard`, `app/console`, and the entire `app/(console)/` tree. None of them shadow a real route, but they're noise and a near-miss conflict (`app/console/` vs `app/(app)/console/`).

### F-21: `app/api/nexis/` is entirely empty route tree

**Where:** `app/api/nexis/auth/{policies,me,users,teams}/`, `app/api/nexis/incidents/{start,latest,[id]/approve}/`, `app/api/nexis/integrations/connectors/`, `app/api/nexis/reset/`. All empty directories. `lib/nexis/services/` likewise empty.
**Repro:** `find app/api/nexis lib/nexis -type f` → nothing. The SDK at `lib/auth.ts`, `lib/audit.ts`, etc. talks directly to the control-plane via `API_URL_INTERNAL`. There is no `app/api/nexis/*` proxy.
**Fix:** Delete the empty `app/api/nexis/` and `lib/nexis/` trees, or decide whether to actually implement the proxy. Right now they're misleading.

### F-22: `Comparison.tsx` light-mode rows have insufficient contrast on alternating bands

**Where:** `components/sections/Comparison.tsx:86` (zebra: `bg-[var(--color-background)]` vs `bg-[var(--color-muted)]/40`)
**Repro:** In light mode `--color-background` is `hsl(0 0% 100%)` (white) and `--color-muted` is `hsl(210 20% 96%)` (near-white). At 40 % opacity the zebra band is `hsl(210 20% 98.4%)` — visually identical to white. The "even/odd" affordance disappears in light mode.
**Fix:** Bump the zebra band to `bg-[var(--color-muted)]` (full opacity) or `bg-[var(--color-muted)]/80`.

### F-23: `Testimonials` initials avatar — color-mix with low-opacity muted in light mode

**Where:** `components/sections/Testimonials.tsx:76` (`bg-[color-mix(in_srgb,var(--color-primary)_15%,var(--color-muted))]`)
**Repro:** Light-mode `--color-muted` is near-white, so the avatar fill becomes `hsl(217 91% 92%)` — pale blue against the muted section background `--color-muted`. Initials are visible but the avatar disc nearly disappears.
**Fix:** Mix against `var(--color-card)` (white) or bump primary contribution to 25 %.

### F-24: Footer logo + tagline column collapses to `col-span-2` on mobile — first column 100 % width then cramped 5-column grid

**Where:** `components/layout/Footer.tsx:15`
**Repro:** `grid-cols-2 md:grid-cols-6` — on mobile the logo block spans both cols (full width), then `Product`, `Resources`, `Company`, `Legal`, `Connect` are split into 2-column pairs. The last column lands alone in a row. Tolerable but ugly.
**Fix:** Either `grid-cols-1` on mobile, or rearrange columns to fit a 2-column mobile layout deliberately.

## Low

### F-25: `Hero.tsx` Skip-to-dashboard pill uses `hidden md:inline-flex` — also hidden on tablet portrait

**Where:** `components/sections/Hero.tsx:108`
**Repro:** The pill never appears <768 px. Probably intentional but breaks the affordance on iPad portrait.
**Fix:** `sm:inline-flex` instead, or render a smaller pill on mobile.

### F-26: `lib/auth.ts` `MFAEnrollResp.secret` is sent to the client for test harnesses

**Where:** `lib/auth.ts:51`
**Repro:** Code comment explicitly notes the secret is round-tripped to the client "so test harnesses can generate a TOTP code without OCR'ing the QR." In production this is the TOTP shared secret in plaintext over the wire to a logged-in user — also visible in any tab-recording / browser-extension content script. Marked "Treat as one-time use" but nothing enforces that.
**Fix:** Gate the `secret` field on a `NEXIS_TEST_HARNESS=1` env (server-side only) so production responses don't include it. Or zero it out at the backend in production.

### F-27: `IntegrationCard.tsx` uses `bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900` for GitHub branding — inverts surface direction

**Where:** `components/integrations/IntegrationCard.tsx:44, 72` (kafka also)
**Repro:** Brand-color overrides on the card avatar contradict every other card. GitHub avatar is dark in light mode and light in dark mode — opposite of all other provider chips.
**Fix:** Either keep a constant brand color (regardless of theme) or theme-align. The current inversion looks deliberate but is inconsistent.

### F-28: Cookie reads don't check expiry

**Where:** Every `document.cookie.match(/nexis_session=…/)` and `nexis_workspace=…` read across `components/workspaces/WorkspaceSwitcher.tsx`, `components/console/Sidebar.tsx:96`, `Topbar.tsx:70`, `QuickActionsRow.tsx:36`, `ProjectsHealthGrid.tsx:36`, `DashboardKpiStrip.tsx:37`, `RecoveryPipelineMini.tsx:75`
**Repro:** The client reads `nexis_session` presence-only. If the cookie is expired but still present (e.g. the browser hasn't garbage-collected it yet), the polling SDK calls will 401 silently. The session cookie is `httpOnly` so the client can't actually read its value — the reads here are only for `nexis_workspace`, which is non-httpOnly. So this is mostly fine, but the pattern needs a guard for the polling hooks that will see a stream of 401s once expired.
**Fix:** On 401 from any SDK poll, redirect to `/sign-in?next=` instead of silent swallow.

### F-29: `Hero.tsx` background dot-grid uses opacity `0.025` on `var(--color-foreground)` — invisible in light mode

**Where:** `components/sections/Hero.tsx:35`
**Repro:** `opacity-[0.025]` of `--color-foreground` (navy in light) is essentially indistinguishable from white background. The atmospheric dot grid the comment promises doesn't render in light mode.
**Fix:** Bump to `opacity-[0.06]` or use `var(--color-border)` directly.

### F-30: Status page `UptimeRibbon` has hardcoded fake data with degraded/down day indices baked in

**Where:** `app/status/page.tsx:97–101`
**Repro:** The "live" status page renders the same 3 fake degraded days every visit (idx 22, 53, 71). The day labels are `90 - idx`, so the "degraded" days drift wrt today's date.
**Fix:** Either wire to real uptime data, or seed the mock from `new Date()` so it doesn't tell visitors there was an incident on a specific date that didn't happen.

### F-31: `next/image` with `unoptimized` on the main Logo

**Where:** `components/ui/Logo.tsx:29` (`unoptimized`)
**Repro:** The logo bypasses Next image optimisation entirely (raw 540×140 SVG served). Tiny SVG, so this is mostly fine — but inconsistent with `ThemeAwareLogo.tsx` which uses the optimiser.
**Fix:** Drop `unoptimized` — SVG is already small, Next handles it.

### F-32: `NotificationsBell` polls with no cleanup on tab-hide

**Where:** `components/console/NotificationsBell.tsx` and several other 10s pollers (`Sidebar.tsx` lines 88–92)
**Repro:** Every console tab polls `/v1/audit`, `/v1/workspaces/.../pipelines`, `/v1/approvals/pending`, integrations, projects — even when the tab is in the background. Multiple tabs multiply this load by N.
**Fix:** Gate the `setInterval` on `document.visibilityState === "visible"` (subscribe to `visibilitychange`). Defer polls when hidden.

## Lint census

Confirmed with `./node_modules/.bin/eslint .`:

| File                                                      | Errors | Warnings |
| --------------------------------------------------------- | -----: | -------: |
| `app/(app)/console/eval/client.tsx`                       |      1 |        0 |
| `app/(app)/console/incidents/[id]/client.tsx`             |      2 |        0 |
| `app/(app)/console/live-demo/client.tsx`                  |      1 |        0 |
| `components/ThemeAwareLogo.tsx`                           |      1 |        0 |
| `components/ThemeToggle.tsx`                              |      1 |        0 |
| `components/approvals/DecisionDialog.tsx`                 |      1 |        0 |
| `components/console/AuditTable.tsx`                       |      0 |        1 |
| `components/console/UserMenu.tsx`                         |      0 |        1 |
| `components/incidents/NasaTlxModal.tsx`                   |      1 |        0 |
| `tests/waitlist.test.ts`                                  |      2 |        0 |
| **Total**                                                 | **10** |    **2** |

Rule breakdown:
- `react-hooks/set-state-in-effect` ×7 (the React 19 / compiler rule against synchronous setState in `useEffect`)
- `react-hooks/purity` ×1 (`Date.now()` during render in `incidents/[id]/client.tsx`)
- `react-hooks/refs` ×1 (reading `.current` during render in the same file)
- `react-hooks/incompatible-library` ×1 (TanStack Table's `useReactTable`, warning only)
- `@typescript-eslint/no-explicit-any` ×2 (waitlist tests)
- `@typescript-eslint/no-unused-vars` ×1 (`router` in `UserMenu.tsx`)

`tsc --noEmit` is clean (zero errors).

## Dead-route census

Routes with file-system presence but zero `<Link>` references and zero redirects in:

| Route                                          | Referenced from                                                         |
| ---------------------------------------------- | ----------------------------------------------------------------------- |
| `/console/dashboard` (does NOT exist)          | `components/sections/Hero.tsx:107`, `app/changelog/page.tsx:134` (copy) |
| `/dashboard` (only a redirect to `/console`)   | `app/(auth)/sign-in/page.tsx:152, 155`, `sign-up/page.tsx:203`          |

Empty directories the user almost certainly meant to delete:

| Path                                       | What's there                                  |
| ------------------------------------------ | --------------------------------------------- |
| `app/admin/`                               | empty                                         |
| `app/console/`                             | empty (note: the real route lives at `app/(app)/console/`) |
| `app/dashboard/`                           | empty                                         |
| `app/(console)/`                           | empty route group with empty sub-trees (`admin/{audit,integrations,policies,teams,users}`, `dashboard/`, `incidents/[id]/`) |
| `app/api/nexis/{auth,incidents,integrations,reset}/...` | empty proxy tree            |
| `lib/nexis/services/`                      | empty                                         |

Routes orphan-checked against `grep -r "href=" app/ components/` — every real route in `app/(app)/console/*` is reachable through the Sidebar, breadcrumb, or a deep link from another page. No unreferenced live routes found.

## Light-mode contrast issues

Confirmed in light theme (`--background: hsl(0 0% 100%)`, `--muted: hsl(210 20% 96%)`):

| Component                                                      | Problem                                                                                                            |
| -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `components/ui/SystemVector.tsx`                               | `var(--text-muted)`, `var(--text-primary)`, `var(--border-hover)` are undefined CSS vars → SVG falls back to black; in light mode that reads as harsh; in dark mode invisible (F-5) |
| `components/ui/ApprovalVector.tsx`                             | Same undefined vars as above (F-6)                                                                                  |
| `components/sections/Comparison.tsx:86`                        | Zebra rows `bg-background` vs `bg-muted/40` — both near-white, no visual band (F-22)                                |
| `components/sections/Testimonials.tsx:76`                      | Avatar fill `color-mix(primary 15% + muted)` → pale-blue disc on near-white section bg, very low contrast (F-23)    |
| `components/sections/Hero.tsx:35`                              | Dot-grid at `opacity-[0.025]` of foreground → invisible in light (F-29)                                             |
| `components/workspaces/WorkspaceSwitcher.tsx:103, 120`         | `bg-gray-300` / `bg-gray-400` skeleton dots — fine in light, invisible in dark (F-16)                               |
| `components/integrations/IntegrationCard.tsx:44`               | `bg-zinc-900 text-white dark:bg-zinc-100` — light-mode GitHub avatar is near-black against light card (intentional brand but jarring vs the rest of the catalog) (F-27) |
| 21 chip components with hardcoded `text-zinc-700 dark:text-zinc-300` | Light-mode shows zinc-700 (low-saturation gray) for "queued"/"pending" pills — readable but breaks the token palette; will drift if `--color-muted-foreground` ever changes (F-14) |
| `app/not-found.tsx`                                            | Uses `text-text-secondary` and `text-primary` Tailwind classes — these DO work (they're via `@theme inline`) but mixing with the new token usage in the rest of the app is inconsistent |

The user's complaint that "landing looked bad in light mode" specifically maps to F-5 (Hero right column SVG diagrams have invisible/wrong labels) and F-22 + F-23 + F-29 (Comparison zebra disappears, Testimonials avatars pale-blue-on-white, Hero dot-grid invisible).

---

## Files referenced (absolute paths)

Source files this audit identifies as needing changes:

- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/Hero.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/layout.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/console/Sidebar.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/ui/SystemVector.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/ui/ApprovalVector.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/eval/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/incidents/[id]/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/live-demo/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/ThemeAwareLogo.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/ThemeToggle.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/approvals/DecisionDialog.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/incidents/NasaTlxModal.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/console/UserMenu.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/console/AuditTable.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/Comparison.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/Testimonials.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/IntegrationsTeaser.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/sections/LiveDemoEmbed.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/workspaces/WorkspaceSwitcher.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/components/integrations/IntegrationCard.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(auth)/sign-in/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(auth)/sign-up/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/dashboard/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/changelog/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/pricing/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/integrations/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/status/page.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/recovery/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(app)/console/workflows/client.tsx`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/lib/auth.ts`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/tests/waitlist.test.ts`

Empty directories that should be deleted:

- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/admin/`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/console/`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/dashboard/`
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/(console)/` (entire tree)
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/app/api/nexis/` (entire tree)
- `/Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/apps/web/lib/nexis/` (entire tree)
