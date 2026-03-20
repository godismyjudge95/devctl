# Changelog

## Unreleased

### Centralised log directory + Logs page

- All managed services now write their logs to a single `~/sites/server/logs/` directory as `<service>.log` files (e.g. `caddy.log`, `dns.log`, `mysql.log`). Previously logs were scattered or missing entirely.
- Logs rotate automatically at 10 MB with 3 backup files kept (`.log.1`, `.log.2`, `.log.3`) — no dependency on system `logrotate`.
- The DNS server now has a dedicated log file (`dns.log`); previously it had none, causing a startup error.
- New **Logs** page in the dashboard (`/logs`): sidebar lists all log files with sizes; clicking a file streams it live via SSE. A **Clear** button truncates the selected file.

## 2026-03-19

### SPX Profiler — speedscope flamegraph

- Replaced the hand-rolled SVG flamegraph and Timeline tabs with an embedded [speedscope](https://github.com/jlfwong/speedscope) iframe (v1.25.0, MIT).
- SPX traces are converted server-side to speedscope's `SampledProfile` JSON format, aggregating ~1.8M raw events into unique stack paths with exclusive wall-time weights. Response is gzip-compressed and typically a few hundred KB.
- New API endpoint: `GET /api/spx/profiles/{key}/speedscope`
- New static asset route: `/speedscope/` serves the embedded speedscope assets without SPA interference.
- Speedscope features available: Time Order, Left Heavy, Sandwich views; minimap; zoom/pan; search; WebGL-accelerated rendering.
- Fixed SPX profiler list card overflowing the viewport on mobile (375px) — added `overflow-hidden` and `min-w-0` constraints; URL now truncates with ellipsis.
- Fixed `<main>` allowing horizontal scroll on mobile (`overflow-x-hidden`).

