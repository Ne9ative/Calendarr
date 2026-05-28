# Design — Auto-setup the *arr stack (qBittorrent download client, root folders, download paths)

- **Date:** 2026-05-28
- **Status:** Approved (pending spec review)
- **Component:** `cmd/server` (new `setup.go`), `web/` (banners)

## Problem

On a fresh install (observed on a test machine), Sonarr had no download client
configured, so grabbing a release did nothing — downloads silently never
started. Calendarr already detects qBittorrent and talks to the Sonarr/Radarr
APIs, so it is well placed to detect and repair this kind of missing wiring
automatically instead of leaving the user with a stack that looks fine but
does not actually download.

## Goal

When Calendarr starts, wire qBittorrent into Sonarr and Radarr as a download
client automatically when it is missing, and surface a guided prompt for the
remaining setup steps that require a user decision (a filesystem path):
the *arr root folder and qBittorrent's download paths.

## Decisions (from brainstorming)

| Question | Decision |
|---|---|
| Scope | Full stack: download client + root folder + qBittorrent download paths, for both Sonarr and Radarr. |
| Trigger | Hybrid — the download client is configured automatically at startup; path-dependent steps (root folder, download paths) are offered via an in-UI banner only when needed. |
| qBittorrent auth | Rely on qBittorrent's localhost auth bypass first; if Sonarr/Radarr reject the client because qBittorrent requires a password, fall back to a prompt that reuses the existing qBittorrent-connect flow, then retry. |
| Code organization | A single new file `cmd/server/setup.go`, package `main`, methods on `*server`. Primitives stay in the client packages (already implemented). |
| Guard | Only auto-add the download client when the service has **zero** download clients. Never modify an instance the user has already configured. |

## Existing building blocks (already implemented, currently unused)

- `sonarr.Client.AddDownloadClient(name, host, port, username, password, category)` — idempotent by name; starts from Sonarr's `QBittorrent` schema, POSTs to `/api/v3/downloadclient`.
- `radarr.Client.AddDownloadClient(...)` — same for Radarr.
- `sonarr.Client.EnsureRootFolder(path)` / `radarr.Client.EnsureRootFolder(path)` — add a root folder if absent.
- `qbit.Client.SetDownloadPaths(savePath, tempPath)` — set qBittorrent's save/temp folders via `setPreferences`.
- `qbit.Detect()` → `Detection{Installed, WebUIEnabled, Port, Username, URL}` (password is hashed in `qBittorrent.ini`, not recoverable).

This feature is mostly **orchestration + wiring + a thin UI**; no new low-level
API client code is required.

## Architecture

New file `cmd/server/setup.go` (package `main`), three methods on `*server`:

```
(s *server) autoSetup()                 // AUTO half — startup goroutine
(s *server) handleSetupStatus(w, r)     // GET  /api/setup/status — feeds the banners
(s *server) handleSetupApply(w, r)      // POST /api/setup/apply  — applies a path-dependent fix
```

New state on the `server` struct, so the banners know what the startup pass
could not finish on its own:

```go
setupMu    sync.Mutex
setupState map[string]string // e.g. "sonarr.downloadClient" -> "auth-failed"
```

Wiring in `main()`:

- `go srv.autoSetup()` added next to the existing `go srv.pollQueue()` /
  `go srv.registerWebhook()` calls (non-blocking, best-effort).
- Two route registrations in the existing handler block:
  `http.HandleFunc("/api/setup/status", srv.handleSetupStatus)` and
  `/api/setup/apply`.

## Phase 1 — auto download client (the core value)

`autoSetup()`, run once per startup in a goroutine:

For each of Sonarr (`s.sc`) and Radarr (`s.rd`) that is non-nil, and only when
`s.qbitDet.Installed`:

1. List the service's download clients.
2. **Guard:** if it already has ≥1 download client, skip (leave the user's
   config untouched) and clear any stale `setupState` entry.
3. Otherwise call `AddDownloadClient` with:
   - `name`: `"qBittorrent"`
   - `host`: `"localhost"`
   - `port`: `s.qbitDet.Port` (default 8080)
   - `username`: `s.qbitDet.Username` (default `"admin"`)
   - `password`: `s.cfg.QbitPass` (may be empty → relies on localhost bypass)
   - `category`: `"tv-sonarr"` for Sonarr, `"radarr"` for Radarr
4. On success: log it; clear `setupState[<svc>.downloadClient]`.
5. On failure: log it; set `setupState[<svc>.downloadClient] = "auth-failed"`
   (the add POSTs without `forceSave`, so Sonarr/Radarr test the connection and
   reject it when qBittorrent authentication fails — that is the signal we want).

**Password fallback:** `handleQbitConnect` (existing, Torrents page) already
takes a password, tests it, and persists it. On a successful connect it will
additionally call `s.autoSetup()` again, which retries the download-client add
now that `cfg.QbitPass` is set.

Phase 1 also includes the minimal `handleSetupStatus` fields needed for a
"download client could not be added — qBittorrent needs a password" banner that
links to the existing qBittorrent password prompt.

## Phase 2 — path-dependent steps (banners)

`handleSetupStatus` (`GET /api/setup/status`) returns, per service:

```json
{
  "sonarr": { "needsDownloadClient": false, "downloadClientError": "", "needsRootFolder": true },
  "radarr": { "needsDownloadClient": false, "downloadClientError": "", "needsRootFolder": false },
  "qbit":   { "needsDownloadPaths": true, "suggestedPath": "..." }
}
```

`handleSetupApply` (`POST /api/setup/apply`) — body `{action, service, path}`:

- `action: "rootfolder"`, `service: "sonarr"|"radarr"`, `path` → `EnsureRootFolder(path)`
- `action: "qbitpaths"`, `path` → `qb.SetDownloadPaths(path, "")`

Suggested defaults proposed by the UI (the user can edit before applying):
- Root folder: qBittorrent's current save path if readable, else blank.
- qBittorrent download path: unchanged unless the user sets one.

## Frontend (`web/`)

- `web/app.js`: on load, fetch `/api/setup/status`; render a dismissible banner
  per actionable item on the relevant page (Calendar/Films/Torrents). Buttons
  call `/api/setup/apply` or open the existing qBittorrent password prompt.
- `web/style.css`: banner styling (reuse existing notice/banner patterns).
- `web/i18n.js`: new strings in the 9 supported languages.

## Error handling & idempotency

- `autoSetup()` is best-effort: every failure is logged and recorded in
  `setupState`; it never blocks startup and never crashes.
- All adds are idempotent (by name) and gated by the zero-client / already-present
  checks, so running every startup is safe and self-healing.
- The zero-download-client guard guarantees Calendarr never overrides a user's
  existing download-client configuration.

## Testing / verification

- `cmd/server/setup_test.go`: unit test the guard logic of `autoSetup()` against
  a mock Sonarr/Radarr served by `httptest` — assert that (a) a service with an
  existing download client is left untouched, and (b) a zero-client service with
  qBittorrent detected triggers exactly one add with the expected payload.
- Manual verification on a clean Sonarr instance: start Calendarr, confirm the
  qBittorrent download client appears in Sonarr → Settings → Download Clients
  and that grabbing a release now starts a download.

## Out of scope

- Configuring indexers in Sonarr/Radarr (that is the existing Prowlarr-connect
  feature's job).
- Changing an existing, user-configured download client.
- Non-qBittorrent download clients.
- Quality profiles / naming / other *arr settings.

## Implementation order

Phase 1 first (auto download client + password-fallback retry + the status
field that drives its banner) — it resolves the actual reported problem. Phase 2
(root folder + qBittorrent download-path banners) follows as a separate slice.
