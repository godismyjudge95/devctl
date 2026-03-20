# Changelog

## 2026-03-19

### SPX Profiler — speedscope flamegraph

- Replaced the hand-rolled SVG flamegraph and Timeline tabs with an embedded [speedscope](https://github.com/jlfwong/speedscope) iframe (v1.25.0, MIT).
- SPX traces are converted server-side to speedscope's `SampledProfile` JSON format, aggregating ~1.8M raw events into unique stack paths with exclusive wall-time weights. Response is gzip-compressed and typically a few hundred KB.
- New API endpoint: `GET /api/spx/profiles/{key}/speedscope`
- New static asset route: `/speedscope/` serves the embedded speedscope assets without SPA interference.
- Speedscope features available: Time Order, Left Heavy, Sandwich views; minimap; zoom/pan; search; WebGL-accelerated rendering.
- Fixed SPX profiler list card overflowing the viewport on mobile (375px) — added `overflow-hidden` and `min-w-0` constraints; URL now truncates with ellipsis.
- Fixed `<main>` allowing horizontal scroll on mobile (`overflow-x-hidden`).

