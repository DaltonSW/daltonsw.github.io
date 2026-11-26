# GameLog tech-debt plan (remaining work from the 2026-11-25 review)

Each numbered item is meant to be its own commit, done in order. Read `CLAUDE.md`,
`tools/gamelog/README.md` and `tools/gamelog/INVARIANTS.md` before starting. Nothing in this plan
may weaken the never-lose-data rules (merge-only archive, `Extra` catch-alls, string dates,
pre-write loss check, no index-shifting removal from `playthroughs[]`).

Line numbers below were taken at review time and may have drifted a little.

## Ground rules and decisions already made

- **Commits:** the user's global rule is no `Co-Authored-By` and no `Claude-Session` lines.
- **The user only uses the web UI** (`go run .` from `tools/gamelog`), never the CLI. The CLI is
  being removed (Phase 2).
- **Every playthrough entry should use `sessions:`.** Flat `started`/`finished` pairs get migrated
  (Phase 1).
- **Keep the written-playthrough-page path** (`written` branch in
  `layouts/partials/playthrough-entries.html`, `archetypes/playthroughs.md`). None exist yet, but
  the user may write some.
- **Deferred, not part of this plan:**
  - Look at what Steam's `GetSchemaForGame`, the extra owned-games fields (`playtime_2weeks`, …)
    and `GetUserStatsForGame` return, then decide whether to capture them.
  - Slimming `/games` further. The Log tab is most of the page's weight. Loading it separately was
    rejected: Log is the default tab, so the bytes still download on a typical visit. Trimming each
    achievement row's markup is the likelier route.
- **Left out on purpose:**
  - Archive `updated`/`fetched`/`last_attempt` changing on every refresh: those dates record when
    each record was last checked.
  - Page speed: Catch up renders in ~0.56s and the Hugo build takes ~1.2s. Not worth optimising.
- **Baseline:** `go test ./...` and `go vet` are clean. Staticcheck shows only the five
  pre-existing nits listed in item 10.
- **CI:** `.github/workflows/gamelog-test.yaml` runs vet and tests on pushes and PRs touching
  `tools/gamelog`. The Hugo deploy runs on pushes to `main`.
- **Tests run offline:** `testutil.RunOffline` fails any test that reaches the internet. Stub new
  endpoints; see INVARIANTS "Tests".

## Open follow-ups (no code needed)

- **Xbox:** check whether the "xbox: account response has no profile" errors (19 of 23 records on
  2026-11-24) stopped now that clients are shared. Run from the desktop, which has `XBLIO_API_KEY`.
- **PSN:** the token (`PSN_NPSSO`) expired around 2026-09-25. The user hasn't played PlayStation
  since, so nothing is being missed. When convenient, renew it in `tools/gamelog/.env` (README
  "PlayStation, via Sony's own trophy API" says how), load PSN from Catch up, and check that
  records now store Sony's verbatim `raw` (`{"definitions": …, "earned": …}`).

**Next:** Phase 3, item 7. Phase 1 is merged. Phase 2 is done on branch `gamelog-remove-cli`
(3 commits). Still to do from Phase 2: the user runs a real Trackmania fetch from the new season
picker.

Notes for later items:
- Item 8: `WriteEntry`/`WriteSplit` are still identical. The `Saved …` prints in `mutate` are still
  there.
- Item 9: the `/suggest` redirect routes still exist.
- Item 10: the TUI comments in `server` are already fixed. The ones in `model`
  (`frontmatter.go`, `playthroughs.go`) remain.

---

## Phase 1: Everything on sessions

Data at review time: 348 flat entries with a start and a finish, 8 flat open runs (start only),
144 already on `sessions:`, 4 planned placeholders (no dates, stay as they are).

**Done (2026-11-25):** all three items. 357 entries were migrated (the extra one was Ori, finish
only, given `started: 2020-03-07`). There turned out to be 0 planned placeholders and 3 undated
status-only entries, which stay without sessions. A finish with no start becomes a one-day session.
The Hugo flat branches were kept as a fallback, since written playthrough pages have no sessions.

### 1. Model changes

- New runs (`AddPlaythrough`), `GraduatePlannedPlaythrough`, `CreateGameFile`'s
  `writeFirstPlaythrough` (`model/games.go:402`) and `CloseLatestOpenRun` (`model/status.go`)
  write or read `sessions:` only.
- Remove the convert-on-first-use branches in `AddSession`, `EditSession`, `SplitSession`,
  `UpdatePlaythrough` (`model/playthroughs.go`) and the matching allowed-removal declarations
  (`mutate.AddSessionAndWrite` is deleted in Phase 2; also check `server_games.go` handlers and
  `server_catchup.go` `logSession`).
- **Hand edits:** a flat entry typed by hand is still accepted. Convert it to a single session
  in memory on `LoadPlaythroughs`. The loss check then needs to know that move is expected (the
  flat `playthroughs[N].started`/`finished` paths vanish), so declare it automatically for
  entries that arrived flat.
- **Session order:** Go picks the "last session" by position (`Playthrough.LatestDate`,
  `FinishDate`, `IsOpen`, `playthroughs.go:241-266`). Hugo sorts by `started`
  (`entry-last-activity.html:13`). Make Go work it out by date too, so they agree.
- Keep `PlaythroughEntry.Started`/`Finished` in the struct for reading only, or drop them if the
  load-time conversion makes that safe. `Extra` must keep working either way.

### 2. One-off migration of the 356 flat entries

A throwaway script, not a feature (CLAUDE.md: "Don't build tooling for one-off work"), e.g. a
temporary `_test.go` or `main` that loads each `playthroughs.yaml`, saves through
`mutate.WriteEntry` (so the loss check runs), and is deleted afterwards. Spot-check a diff,
including an open run (`finished: ""` must stay as the key). Commit the data separately.

### 3. Hugo and docs

- `layouts/partials/playthrough-entries.html` and `entry-last-activity.html` lose their flat
  branch; keep a small fallback for a hand-edited flat entry. Check `games-timeline.html`,
  `games-log.html`, `games/term.html` and `game-first-finished.html` for flat reads.
- Update the README sections "How playthroughs are written" and "Planned replays", and the
  INVARIANTS rules about converting to `sessions:`.

---

## Phase 2: Trackmania season picker, then remove the CLI

### 4. Season picker on the Trackmania game page

The web UI's Trackmania refresh (`server_games.go:1285` `handleRefreshNadeo` →
`commands.SaveNadeo(ctx, gamesDir, nadeo.FetchOptions{}, "")`) only fetches the current season.
Add a picker that lists every official campaign with how much of it is archived (what
`commands.RunNadeoSeasons` prints today, `internal/commands/nadeo.go`), lets you tick seasons, and
runs the fetch as a background job (job registry in `server_jobs.go`). Reuse
`nadeo.FetchOptions` season selection (`providers/nadeo/fetch.go`) and keep `mergeNadeoRecord`'s
union-by-season guarantee. Credentials belong to a real player account: keep `nadeoMinInterval`
and make the default selection the current season only.

### 5. Delete the CLI

- `main.go` runs the server directly, keeping a `--port` flag (plain `flag` package).
- **Delete:**
  - `cmd/`.
  - The `Run*` functions in `internal/commands`: `RunSuggest`, `RunAchievements`,
    `runAchievementsAll`, `RunProject` (check what Housekeeping's Regenerate summaries,
    `server_provider.go` `handleProject`, calls first and keep that logic), `RunExophase`,
    `RunPSN`, `RunNadeo*`, `RunNinjaKiwiFetch`.
  - `nadeo_progress.go`.
  - The terminal formatters: `FormatSuggestionReport`, `formatRABlock`, `formatSteamBlock`, and
    the psn/exophase list formatters plus their tests.
  - `resolveSuggestSlug` and `joinSlugs`.
  - `maybePromptNewSession`.
  - `mutate.ConfirmAndWrite`, `AddSessionAndWrite`, `PreviewEntry`.
  - In `forms`: `SessionForm`, `ConfirmWrite`, `SelectExistingGame`, `OrNone`.
  - `externalid.ValidateExternalID`.
  - `GameSummary.SuggestLabel` and `Label` (check `Label` callers).
  - Nadeo's `--dump-raw` (`nadeo.go:94-98,318-340`). Fixtures would then be saved by hand.
- `go mod tidy` should drop cobra, huh, bubbletea, bubbles, lipgloss and go-isatty.
- **Rewrite the README:** the CLI usage block, the `gamelog suggest` / `achievements` / `project`
  / `psn list` / `exophase list` / `nadeo …` sections (re-point ID lookups at the web UI's New
  games linking), and the "Closing the gap with the timeline" one-shot nudge paragraph. Also the
  INVARIANTS "Current state" CLI list.

### 6. Fold what's left of `forms` into `model`

After item 5, `internal/forms` is just validation (`ValidateDate`, `ValidateRating`, `Today`)
and the status lists (`GameStatuses`, `PlaythroughStatuses`, `IsOneShot`,
`DefaultPlaythroughStatus`, quick-status lists, `ParseSubgames`, `OrDash`). Move the status lists
next to `model/status.go`. `model.IsHandSetStatus` (`status.go:17`) is identical to
`forms.IsOneShot`; keep one. Update INVARIANTS' "Adding another one-shot status means changing
four places" (it was really five).

---

## Phase 3: Removing duplication

### 7. Provider list in one place

The six achievement providers are listed by hand in:

- `model.GameSummary`: six ID fields plus `ProviderIDs()` (`model/games.go:37-79`), filled in
  `GameSummaryFor`.
- `model.NewGameFields`: six fields plus `SetProviderID` switch plus `formatNewGameFile`
  (`games.go:299-372`).
- `Doc.providerField` (`frontmatter.go:294`). This one is fine as the single front-matter mapping.
- `SuggestLabel` (deleted in item 5).
- Hugo: `games-list.html:6-14` and `games-log.html:9-17` hardcode order and names. Their order
  (RA, Steam, **Xbox, PSN**, Ubisoft, GP) differs from Go's `ProviderOrder` (RA, Steam, **PSN,
  Xbox**, …).

Give `GameSummary` and `NewGameFields` a provider-keyed `IDs map[string]string` (templates and
server code reading `.SteamAppID` etc. need updating). Add `data/providers.yaml` (key, label,
order) for Hugo and settle on one order. README "Linking a game to external IDs" lists where a new
provider must be wired; update it.

### 8. Small server and mutate merges

- `mutate.WriteEntry` and `WriteSplit` (`mutate.go:156,170`) are byte-for-byte identical. Merge
  them.
- `server.go:325-404`: `renderRow`, `renderPlaythroughFragment`, `renderJobProgress` are special
  cases of `renderFragment`, each with its own cache var (`server.go:49-54`, built in `init`).
  Replace them with a fragment spec map like `pageSpecs`.
- `model.SaveIgnored` (`model/ignored.go`) uses `os.WriteFile`. Use `writeFileAtomic` like
  `SaveCatchup`.
- `internal/commands/review.go` now only holds `DeleteGameStub` and `HasArchiveRecord` (its name
  comes from the retired review flow). Move or rename them.
- `mutate` and `model` print `Saved …` to stdout from library code (`mutate.go:102,163,177`).
  Drop the prints, since the server reports through flashes and job logs.

---

## Phase 4: Dead code and stale docs

### 9. Delete unused files and routes

- `data/colors.yml` and `data/supported_icons.yml` (theme leftovers, no layout references them).
- The legacy `/suggest` and `/suggest/{slug}` redirect routes (`server.go:189-190`,
  `server_provider.go:27-38`).
- `hugo.toml`: the comment above `[params.providers.*]` points at
  `layouts/partials/games-overview.html`, which doesn't exist. The params are used by
  `games-list.html:75`.
- `LoadBTD6Summary`/`LoadCampaignSummary` are only called by tests. Fine to keep as test helpers.

### 10. Bring docs and comments up to date

- INVARIANTS "Current state (checked 2026-11-08)": now 556 bundles, 424 `playthroughs.yaml`, 507
  summaries, 0 drafts; archive 545 records (435 Steam, 48 RA, 23 Xbox, 13 PSN, 18 Google Play, 5
  Ubisoft, 1 Nadeo, 1 Ninja Kiwi). Recount when editing.
- INVARIANTS says subset attaching is "a Housekeeping button"; it's on Catch up's New games.
- Stale comments:
  - "RetroAchievements has no last played signal" (`model/archive.go` in `WriteAchievementSummary`,
    `layouts/partials/game-last-played.html`). RA now uses `API_GetUserRecentlyPlayedGames`.
  - `AchievementSummary` doc "both Steam-only stats" (`archive.go:488`).
  - `gameDateRange` comment about falling back to front matter (`playthroughs.go:285-293`).
  - `FrontMatter.PSNID` "fills this from a public Exophase profile" and `XboxID` "from a public
    OpenXBL export" (`frontmatter.go:36-51`).
  - `ProviderOrder` "~450 Trackmania campaign tracks"; it's 625 (`archive.go:113-116`,
    `frontmatter.go:64`).
  - Many "TUI" references (`server.go:86-90,110,427`, `playthroughs.go`, `frontmatter.go:105`).
- staticcheck nits: `server.go:39` (a comment line starting `// go:embed` is flagged SA9009,
  reword it), `steam/localschema.go:104` (S1021), capitalised error strings at
  `server_bulk.go:60` and `server_games.go:480`, unused `locked` in `achievements_test.go:66`.
