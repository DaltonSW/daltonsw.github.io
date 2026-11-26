# gamelog — invariants

The rules in `tools/gamelog/` that must not be undone, and the known limits behind them, written
for someone picking this up fresh.

Read `CLAUDE.md` at the repo root first. It states the intent these are judged against: **this is
a durable archive of gameplay history, and losing captured data is the worst thing that can
happen.** `README.md` documents the provider APIs in more depth; this file is the short list of
what's load-bearing.

## Current state (checked 2026-11-08)

- `content/games/` holds **540 game bundles**. 492 have an `achievement-summary.yaml` and 358
  have a `playthroughs.yaml`. A missing `playthroughs.yaml` is legal: `LoadPlaythroughs` returns
  an empty file, and "nothing logged yet" is a real state. 123 games are still `draft: true`,
  and the games list's "drafts only" filter is how to walk them.
- `archive/` holds **529 records (17 MB)**: 434 Steam, 47 RetroAchievements, 23 Xbox 360,
  13 PSN, 5 Google Play, 5 Ubisoft, 1 Trackmania (Nadeo), 1 BTD6 (Ninja Kiwi).
- The day-to-day tool is the web UI (`gamelog serve`). Its home page, Catch up, runs one "Load
  what's new" job (scan, refresh linked games into the archive, and BTD6, over the ticked regular
  sources: Steam, RA, Google Play, BTD6), then lists what to act on in place: unlogged
  activity read from the archive, new or unlinked games (including subset attaching), stale-game
  triage, and runs left open. Housekeeping keeps the owned-but-unplayed backlog import, the
  ignore list, achievement completion, and the manual refresh tools. The CLI still has `suggest`, `achievements`,
  `project`, `exophase`, `psn`, `nadeo` and `ninjakiwi`. The old CLI `scan`, `stale` and `review`
  flows are gone, so don't go looking for them.
- The status vocabulary is split in two on purpose (see `internal/forms/forms.go`):
  `GameStatuses` (front matter, 9 values, including `backlog` and `software`) and
  `PlaythroughStatuses` (entries in `playthroughs.yaml`, 8 values, including `misc_launch`).
  `planned` is a third, placeholder-only entry status for planned replays. `software` is
  game-level only: it describes the whole record, not a mode a game can have alongside others.
  "Unplayed" is not a status at all. The site derives it (`layouts/partials/game-unplayed.html`)
  for any game with no playthrough entries and no captured playtime, and shows it as a tag.
  A game's front matter carries **no dates**. Started, first finished and last played are derived
  from `playthroughs.yaml`, so a date that belongs to the game belongs in an entry. Creation writes
  dates as a first playthrough (`CreateGameFile`), and stale triage closes the sole open entry
  rather than stamping the game. `legacy_last_played:` on 13 games is inert and preserved via
  `Extra`. Don't reintroduce `started`/`finished` on `FrontMatter`. An entry's `finished` is the day
  the run ended; its optional `beaten:` is the day it beat the game when that's earlier, and is
  what first finished reads (`firstFinished` and `game-first-finished.html`, kept in step).
  **A game's status is derived from its runs** (`model.DeriveGameStatus`):
  - Any open run means the most recently active one's status.
  - Otherwise `finished` if any run was finished, else the status of the run that ended last.
  - Runs that don't count: `planned`, `misc_launch`, and `endless`/`multiplayer` side modes.
  - It stays hand-set for one-shot games (`endless`/`multiplayer`/`software` describe the kind of
    game, not an outcome) and for games with no counted runs.
  - The edit forms show a derived status read-only.
  "Mastered" isn't a status either. It's derived from the archive (`model.DeriveMastery` →
  `achievement-summary.yaml`'s `mastered:`) and shown as a badge by
  `layouts/partials/game-mastery.html`, so a mastered game can still be `playing` a replay.

---

## Invariants: don't "fix" these

### Data safety

- **Achievement refreshes merge, never overwrite.** `mergeProvider` (`internal/model/archive.go`)
  keeps every unlock once recorded, and a fetch that errors or returns no achievements leaves the
  achievement data alone. Activity fields (playtime, last played, total, platform, icon,
  completion) still merge on such a fetch via `mergeActivity`, since providers read them from a
  separate listing, and only by rules that can't shrink them: max, latest, fill-blank. Gating
  them on the achievement fetch once froze playtime for every Steam game without achievements.
  The regression tests in `internal/model/archive_test.go` cover this. Keep them.
- **Mastery milestones are only ever added.** `ProviderRecord.Mastery` is persisted because it can
  stop being derivable: a DLC turns 50/50 into 50/60, and an RA set revision demotes `award_kind`.
  `mergeProvider` unions in what the *old* record qualified for before applying a fetch, and
  `unionMastery` keeps the earliest date. `TestMergeProvider_DLCNeverErasesAMastery` covers this.
  Don't turn `Mastery` into a value recomputed from the current achievement list.
- **`Extra map[string]any` with `yaml:",inline"` is load-bearing** on `PlaythroughEntry`,
  `SessionEntry` and `FrontMatter`. It's the only thing keeping a hand-added field from being
  deleted by the next whole-file rewrite.
- **Every run's dates are a `sessions:` list; nothing writes a flat `started`/`finished`.** A flat
  pair typed by hand is moved into one session on load (`convertFlat`), and the loss check lets
  those flat paths go only when `sessions[0]` holds the value (`PlaythroughsFile.ConvertedMoves`).
  The "latest" session is the one with the greatest `started` (`latestSession`), never the last by
  position, because Hugo sorts by `started` and takes the last. An entry with no sessions is not
  open. `TestLoadPlaythroughs_ConvertsFlatEntriesToSessions` pins the conversion.
- **Dates in `playthroughs.yaml` are typed as `string` on purpose.** YAML resolves an unquoted
  `2026-01-04` to a timestamp, and re-encoding that rewrites every date in the file as RFC3339.
- **Every playthrough write goes through the pre-write loss check.** `mutate.VerifyNoLoss` diffs
  the pending rewrite against the bytes on disk and refuses if a field would vanish. It's reached
  via `WriteEntry`/`WriteSplit`/`ConfirmAndWrite`/`WriteFrontMatter`. Operations that
  legitimately remove a path declare it (`RemoveSessionAllowedPaths`, `MoveSessionAllowedPaths`,
  `TruncateSessionAllowedPaths`, or explicit lists in the handlers). If a new operation trips the
  check, the operation is wrong. Don't widen the allowlist to silence it.
- **Playthrough writes are atomic** (temp file + `os.Rename` in `PlaythroughsFile.Save`). It's a
  whole-file rewrite of the only copy of that data.
- **Nothing removes an entry from `playthroughs[]` by index.** `SplitPlaythrough` appends at the
  true end of the slice so no existing entry's index, and so no path into a later entry's nested
  `sessions[]`, ever moves. The allowed-path helpers only diff flat per-session fields and don't
  generalize to a shifted entry. Sessions *can* be deleted and reordered, because that happens
  within one entry. `GraduatePlannedPlaythrough` mutates in place for the same reason, and
  planned replays have no delete path: removing one is a hand edit, which the file's generated
  banner allows.
- **The archive is keyed by provider ID, not slug, and lives outside `content/`.** Slugs change,
  and anything inside `content/` gets published. Hugo copies page-bundle resources unconditionally,
  which is how the old layout once published every raw API response. `playthroughs.yaml` and
  `achievement-summary.yaml` stay unpublished via the `_build.publishResources: false` cascade in
  `content/games/_index.md`.
- **Status flows from runs to the game, never the other way.** `model.DeriveGameStatus` decides
  the game's status, and `mutate.SyncGameStatus` writes it into front matter after every
  playthrough write (`WriteEntry`/`WriteSplit`, so `close.go` and `AddSessionAndWrite` too) and for
  every game in `gamelog project`. The old `SyncStatus`, which pushed a game status onto its sole
  entry, is gone. Don't add a path that edits a derived game status directly: change a run.
  Stale triage does exactly that (`CloseLatestOpenRun`).
- **Playthrough boundaries are confirmed by hand, never derived.** A gap in achievement unlocks
  marks an activity arc, not a playthrough, so nothing should split entries automatically.

### Catch up runs

- **A source loaded on its own is never recorded as a run.** PSN, Xbox, Ubisoft and Trackmania
  each have their own button (`catchupOwnButton`), and `RecordRun` moves the "new since" boundary
  for every source. Recording a one-source load would hide unreviewed activity on all the others.
  For the same reason, a main run with nothing ticked is refused.
- **Provider clients are shared per process** (`internal/commands/clients.go`, plus
  `ExophaseClientFor`). Don't build one per game: that repeats library listings and logins, and
  defeats the RA throttle. Any state on a client needs a mutex, because the scan and the server use
  them concurrently. Account-wide title lists go through `cachedList`. `ForgetScanCache` resets
  the lists and the clients.

### Status vocabulary

- **One-shot statuses gate per entry-status, per platform.** `forms.IsOneShot` (endless,
  multiplayer, software) is enforced by `mutate.OneShotConflict`: at most one entry of a given
  one-shot status per platform, while a *different*-status entry is a real second mode and is
  allowed. **Every entry carries its own status.** The 57 old endless/multiplayer entries that said
  `playing` and leaned on the game's status were rewritten on 2026-11-21, and the code and template
  special cases that resolved them were deleted. Don't write a `playing` entry meaning
  endless/multiplayer again. The one exception is `software`: it's game-level only, so a software
  game's entries say `playing`, and the timeline/year rows read them as software.
- **Stale triage only looks at game-level `playing`** (`FindStaleCandidates` in
  `internal/commands/stale.go`), so endless/multiplayer/software games are never offered for
  finished/dropped triage, whatever their entries say.
- **Adding another one-shot status means changing four places together:** `forms.IsOneShot`,
  `PlaythroughStatuses` (only if it's a real per-entry mode), `NO_CONNECTOR_CLASSES` in
  `assets/js/games-timeline.ts`, and the status colour maps in `assets/scss/main.scss` and
  `_vars.scss`.

### RetroAchievements

- **RA 403s Go's default User-Agent.** `raUserAgent` must stay set on every request. `curl` does
  not reproduce the failure.
- **`a=1` is required** on `API_GetGameInfoAndUserProgress.php`, or the award fields come back
  null and every suggestion silently drops to medium confidence.
- **Award dates are RFC3339; per-achievement dates are zoneless SQL datetimes.** They need two
  parsers on purpose: `ParseRAAwardDate` and `ParseRADate`.
- **An unknown RA game ID returns HTTP 200 with `[]`**, not a 404.
- **`UserTotalPlaytime` is in seconds**, unlike the minutes stored everywhere else, so
  `FetchRecord` divides by 60. It's client-tracked and only exists from late 2025, so its absence
  on older games is legitimate.
- **`last_played` comes from a second endpoint on purpose.** `API_GetUserRecentlyPlayedGames` is
  fetched once per run and cached for 10 minutes, or until `ForgetScanCache` (`raRecentlyPlayed`
  in `internal/commands/achievements.go`). A failure is cached the same way, so it warns once.
  Don't collapse it into the per-game fetch, because the ~1.2s throttle multiplies. Don't drop it
  as redundant with the newest unlock date either, because that equivalence was the bug it fixed.
  `c` is capped at 50 server-side, so the paging loop is required.
- **RA rate-limits sustained bursts with 429.** `RAClient` throttles to ~1.2s and retries.
- **A subset is linked by `ParentGameID`, never by its title.** The `[Subset - …]` title only
  *spots* a candidate. `commands.AttachSubset` re-fetches the per-game endpoint and refuses to
  write unless the parent matches the target game's own `retroachievements_id`.
- **Subset counts stay out of a game's headline `unlocked`/`total`.** They live under `subsets:`
  in `achievement-summary.yaml`, because summing a bonus set into the base set describes a set
  that exists nowhere. Subset playtime is also left out, since RA reports it per game id and
  summing it double-counts. `last_played` *is* folded in.
- **`SaveAchievements` walks links in order rather than one id per provider.** A subset is the
  one case of two ids on one provider, so a `map[provider]id` would drop all but the last. It also
  passes a blank title for a subset record, so RA's name for the set isn't overwritten with the
  game's.
- **Nothing detaches a subset from the tool.** Attaching is a Housekeeping button. Undoing it
  means deleting the id from `retroachievements_subsets:` by hand. The archive record is keyed by
  provider id and survives either way.
- **A subset's parent lookup is memoized per process, not persisted.** `raParentCache` in
  `internal/commands/subsets.go` saves Housekeeping a ~1.2s RA call per unattached subset row on
  each render, but every restart of `gamelog serve` pays it again. Fine at one subset; it needs a
  persisted cache before the count grows much.

### Steam

- **Steam puts useful JSON in 400/401/403 bodies.** Read the body; don't bail on the status.
- **Steam needs a SteamID64**, not a vanity name. `SteamClient.resolveSteamID` handles both.
- **Hidden-achievement text comes from the local Steam cache, and only fills blanks.** The Web API
  blanks it; `LoadLocalSchema` reads `UserGameStatsSchema_<appid>.bin`. A missing or unparseable
  file must fall back silently to the API result, never fail the fetch or overwrite API text.

- **Per-game stats merge by key and are never dropped, but a value is the latest reading.**
  `mergeStats` runs in `mergeActivity`, not the achievement merge, because a game can have stats and
  no achievements. A lower fresh value replaces a higher archived one on purpose (games reset and
  spend stats); a key or a value a fetch didn't mention is kept. A `nil` `Value` means Steam reported
  nothing for a stat still at its default, so don't turn it into 0.
- **A 500 from `GetPlayerAchievements` is "no achievements" only with the stats call vouching for it.**
  Apps with stats but no achievements answer 500 rather than 400. `FetchRecord` accepts that only when
  `GetUserStatsForGame` returned 200 listing no achievements. Don't widen it to all 500s: that call
  is also how a real outage looks.

### Exophase (Ubisoft, Google Play)

- **Exophase 403s Go's default User-Agent** (Cloudflare), so requests must send a browser one.
- **The JSON API omits `canonical_id`**, the ID the archive keys on. It exists only in the
  `window.playerGames` payload in the profile HTML, so the client joins the two on `master_id`. A
  game whose canonical ID can't be resolved is **skipped, not guessed at**.
- **Only the account page carries per-service player ids.** The per-service pages don't, and the
  per-service username differs from the account name.
- **The games list needs `?environment=android` for Google Play**, or it answers
  `{"success":false}`. Ubisoft's is the same either way.
- **Google Play achievements are keyed on Google's achievement id, Ubisoft's on Exophase's slug.**
  Don't switch Ubisoft over: its archive is already slug-keyed, and changing the key would count
  every unlock twice on merge.
- **Locked awards come from the game's page, and only Ubisoft archives them.** The earned feed
  lists only earned awards. A Ubisoft row's link tail is the slug the feed reports once it's
  earned, so the two merge. No page carries Google's id, so a locked Google Play award would get a
  key that never matches its unlock, and be counted twice. Leave those out rather than inventing a
  key. Ubisoft's page is `/game/<slug>/challenges/`, not `/achievements/`.
- **The canonical-ID map only covers the first 50 games per service.** The embedded
  `window.playerGames` payload isn't paginated, and `?page=2` returns the same 50. Google Play
  gets past it by decoding the app id out of a game's award ids (see the README's Google Play
  section). Ubisoft has no fallback, so a Ubisoft game past the 50th is skipped, not mis-keyed.
- **Nintendo is on the profile but deliberately not archived.** Switch has no achievements (only
  playtime and last-played), so it needs a record shape that doesn't pretend otherwise, as Nadeo
  and Ninja Kiwi each got, before it's added.

### PSN (Sony's own trophy API)

- **The OAuth `authorize` step returns its code on a 302 to a non-resolvable app-scheme URI.** The
  client reads `code` off the `Location` header instead of following it
  (`http.ErrUseLastResponse`).
- **`npServiceName` must be `"trophy"` on every per-title call for PS3/PS4/Vita titles**, or the
  API answers as if the title doesn't exist. The client always sends what `trophyTitles` reported.
- **The npsso→token exchange is in-memory only, per client.** There is deliberately no refresh
  token cached across runs. The client is shared for the process, so a 401 re-exchanges once.
- The PSN records that predate this client were captured via Exophase. They were deleted and
  refetched, not merged: the two sources key trophies differently (slug vs numeric `trophyId`),
  and merging would have kept every trophy twice.

### Xbox 360 (OpenXBL)

- **Legacy 360 titles must use `/achievements/x360/{xuid}/title/{titleId}`.** The modern
  endpoint returns an empty list for them.
- **That endpoint only lists unlocked achievements**, so the total and last-played date come from
  `player/titleHistory`.
- **Only 360 titles are captured.** Scan leaves out other Xbox titles with a notice naming them,
  because the x360 endpoint would archive an empty record that looks like a real 0/N. Supporting
  modern titles means a second fetch path in `providers/xbox`, verified against a live modern
  title first.

### Providers that aren't achievements (Nadeo, Ninja Kiwi)

- **Neither is in `ProviderOrder` or produced by `BuildProviderLinks`,** and each has its own
  front-matter accessor (`nadeo_account_id`, `ninjakiwi_user_id`). Everything walking
  `ProviderLinks` sums an `unlocked`/`total` pair. Folding 625 Trackmania tracks or a BTD6 save
  into that would restate the game's achievement count as something that doesn't exist.
- **Each has its own merge, and the merge rules are per field.** Check field semantics case by
  case rather than defaulting to "max wins":
  - A Trackmania personal best merges by **minimum** (`mergeNadeoTrack`), because a lap time is
    better when it's smaller. `WorldRank` moves only with the time.
  - An *identical* Trackmania time is the same lap seen twice, and the merge backfills whichever
    half (Core: date/medal/replay; Live: rank) was missing. An *improved* time replaces every
    run-scoped field wholesale, blanks included. That's the one place clearing a field is correct.
  - Ninja Kiwi counters that only grow merge by max. Spendable or consumable values (monkey money,
    current trophies, insta-monkey and power inventory) take the fresh value. Unlock maps merge by
    OR (`mergeNinjaKiwiRecord`).
- **`mergeNadeoRecord` unions campaigns by `SeasonUID` and tracks by `MapUID`,** which is the only
  thing that makes a single-season `nadeo fetch` safe.
- **Nadeo credentials belong to a real player's account.** `fetch` defaults to the current season,
  `nadeoMinInterval` is 500ms, and `--all` is a deliberate, rare choice.
- **Trackmania times are captured at two scopes, `season_best_ms` and `personal_best_ms`, via two
  requests.** `mapIdList` and `seasonIdList` intersect if sent together, and results must be
  filtered on `scopeType` regardless.
- **A `recordScore.time` of `4294967295` (`math.MaxUint32`) is a sentinel**, not a lap.
  `AccountRecord.Usable` filters it.
- **Nadeo batch responses are matched by `mapUid`+`groupUid`, never by position.** Invalid pairs
  are omitted, and the 50-map cap silently truncates.
- **The Nadeo leaderboard endpoint intermittently returns `[]`.** `recordBatch` retries once, and
  never more.
- **`raw` is stored once per record and replaced wholesale** (Nadeo and Ninja Kiwi alike).
  Attaching Nadeo's cross-season batches to every campaign once stored the same ~500KB payload
  25 times.
- **Nadeo tokens are in-memory only, per audience.** `NadeoServices` and `NadeoLiveServices`
  tokens are not interchangeable.

### Hugo side

- **The per-entry "last activity" rule lives in one place**, `layouts/partials/entry-last-activity.html`
  (the latest session's finish or start, otherwise the entry's own). `game-last-played.html`,
  `games-timeline.html` and `games-log.html` all call it. Don't inline it again.
- **Only `game-last-played.html` maxes `achievement-summary.yaml`'s `last_played` against logged
  playthroughs.** That's intentional: a refreshed archive can legitimately be more recent than any
  logged session, so don't make it agree with `playthroughs.yaml`. The timeline and year rows stay
  on logged activity only, because stretching a bar or a year span to an archive date would draw
  play that has no session behind it.

### Tests

- **Tests never reach the internet.** Every package that makes HTTP calls runs its tests through
  `testutil.RunOffline` (`main_test.go`). It refuses non-loopback requests and fails the run even
  when the code swallowed the error. If it fires, stub the endpoint it names. Don't loosen the
  guard. Live checks belong outside `go test ./...`.

---

## History

Kept only because the invariants above point back to these. The details are in git history.

- **Front-matter line-splicing destroyed or misattributed fields.** Guessed YAML field boundaries
  deleted or re-parented data while producing valid YAML and exiting 0. Retired by moving
  playthroughs to a whole-file `playthroughs.yaml`, which is where the loss check, `Extra` and
  string dates come from. `TestAddSession_ConvertsWithoutDisturbingSiblings` (now a hand-typed flat file
  converting on load) and `TestAddSession_LeavesTheEarlierSessionAlone` carry the original repros.
- **The archive used to live at `content/games/<slug>/achievements.json`.** Renames moved it,
  deletes destroyed it, and Hugo published it, `raw` and all. It moved to
  `archive/<provider>/<id>.json`.
- **Slugs were lossy** (`Ōkami` → `kami`, and an empty slug could target the section's own
  `_index.md`). `Slugify` now folds accents, and `validateSlug` rejects empty slugs and path
  separators inside `CreateGameFile`. `TestSlugify` pins the `™`/`®` cases.
- **`raw` responses are kept in full.** This was a decision, not a code change.
- **Nothing used to render the archive.** `gamelog project` now projects it into
  `achievement-summary.yaml` (totals, per-provider breakdown, subsets, and the `earned` list),
  which the games list and each game's page read.
- **Every game had exactly one playthrough entry.** The split/session tools now exist and have
  been used on most of the flagged games. Whether to split the rest is the owner's call.
