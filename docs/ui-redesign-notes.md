# UI redesign notes — Yerd / Lerd

Research date: 2026-08-14

## What makes Yerd feel finished

- Cool gray canvas, slightly darker sidebar, white surfaces. Not one flat white sheet.
- Page titles are small, tracked, uppercase. A one-line subtitle sits under them.
- Sidebar is grouped (Environment / Developer / System) with a soft lavender active pill.
- Status is a 6px dot plus italic lowercase text (`running`, `idle`, `not installed`). Not a fat green badge.
- Meta is chips: PHP 8.5, HTTPS (green lock), HTTP, `/public`, `default`.
- Lists live inside a rounded surface with hairline row rules. No heavy table chrome.
- Primary actions are near-black. Secondary is a quiet outline.
- Footer of the sidebar: green dot + "Daemon connected".
- Settings rows: label + description on the left, control on the right.

## What makes Lerd feel operational

- Status dots on every row (green / gray / yellow).
- Live indicator next to log streams.
- Compact header with version chips and colored toggles.
- Density without clutter. Information first.

## What was wrong with the old dashboard

- Generic shadcn defaults: same card, same badge, same table, every page.
- Sidebar and canvas the same white. No depth.
- Status as bulky colored pills that dominate the row.
- Page titles as large SaaS H1s with no subtitle or grouping.
- Service names as plain text. Existing brand SVGs unused on the main list.
- Inconsistent action patterns and leftover "vibe coded" spacing.

## Direction for this pass

Native control panel. Same world as TablePlus / Yerd, not a SaaS onboarding wizard.

- Geist + Geist Mono stay.
- Near-black primary, lavender active nav, green for healthy, amber for warning.
- Every page: kicker title, subtitle, one surface language.
- Tables stay (e2e depends on them) but look like Yerd lists.
- Preserve all existing behaviour and test-visible strings.

## View checklist

- [x] App shell / sidebar
- [x] Services
- [x] Sites
- [x] Dumps
- [x] Mail
- [x] Logs
- [x] Settings
- [x] Profiler
- [x] WhoDB
- [x] Storage
- [x] Config editor
- [x] Dialogs / sheets / primitives
