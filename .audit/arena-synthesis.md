# Arena synthesis: host boundary

Base: candidate 3 (`dist` + elevate helper ops). [Elevate-ops OS boundary](934ac695-e91f-4211-b9a4-fc1f708c8a63).

Why: privileged mutations already have a door (`devctl helper`). Download tokens and listen ports are unprivileged facts. Mixing them on a Host type lets an installer import elevate-shaped methods. The public surface of candidate 3 is two functions. That is deeper than a 15-method Host.

## Grafts

From candidate 2 ([Platform spec](0b4e7e19-6aeb-4e4e-b8bc-02c6827dc663)):
- No build tags. `assetFor(goos, goarch, name)` so Linux tests can assert Darwin tokens.
- Missing Darwin binaries are omitted rows (`ErrUnsupported`), not a second skip list.
- Delete unused `install.systemctl` helpers so agents do not copy them.
- `EnsureHTTPServer` always writes listen from `dist.ListenHTTP()` so a Darwin box cannot keep Linux `:80`/`:443` in autosave.

From candidate 1 ([Host interface](a4df93a4-ca05-4b74-85c7-ce2eb6ede265)):
- `Archive.File` as the cache basename (already in candidate 3 `Asset.File`).
- Keep parent-file writes as one owner in elevate (`write-unit` vs `write-plist`, not two copies in selfinstall).
- PHP Darwin source stays a fact next to the php token (Origin later in `php/`, not a Host method).

## Rejected

- Candidate 1 Host interface, build tags, stop re-execing helper, `SystemPackages` keeping apt in installer flow.
- Candidate 2 `Platform` bag (`CaddyHTTP`, `OpenCmd`, `HasAPT`, magic bytes on one struct). Elevate Kind-switches. Dist does not grow into OSSpec.
- `Assets.Token` plus installer sprintf of vendor dialects is accepted only because URL templates differ per vendor. Tests lock the full URL string at each installer so a wrong token fails.

## Defaults

- LaunchAgent path: `{siteHome}/Library/LaunchAgents/ai.devctl.plist`.
- `dist` package name, not `platform` or `host`.
- Linux/amd64 tokens stay byte-identical to today's URL fragments.

Cross-judge: [Host design judge](f22e041a-863e-4095-8748-d349650893a1) still running. Parent pick stands unless the judge shows a red flag this note missed.
