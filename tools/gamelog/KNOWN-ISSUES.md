# gamelog — known issues

What's still open in `tools/gamelog/`, and the invariants that must not be undone, written for
someone picking this up fresh.

Read `CLAUDE.md` at the repo root first. It states the intent these are judged against: **this is
a durable archive of gameplay history, and losing captured data is the worst thing that can
happen.** `README.md` documents the provider APIs in more depth; this file is the short list of
what's unfinished and what's load-bearing.

## Current state (checked 2026-09-27)

- `content/games/` holds **528 game bundles**. 480 have an `achievement-summary.yaml` and 339
  have a `playthroughs.yaml`. A missing `playthroughs.yaml` is legal: `LoadPlaythroughs` returns
  an empty file, and "nothing logged yet" is a real state. 243 games are still `draft: true`,
  and the games list's "drafts only" filter is how to walk them.
- `archive/` holds **511 records (17 MB)**: 426 Steam, 45 RetroAchievements, 23 Xbox 360,
  13 PSN, 2 Ubisoft, 1 Trackmania (Nadeo), 1 BTD6 (Ninja Kiwi).
- The day-to-day tool is the web UI (`gamelog serve`). Its Housekeeping page runs the scan for
  unlogged games, the owned-but-unplayed backlog import, stale-game triage, subset attaching,
  and the refresh-all and suggest-all sweeps. The CLI still has `suggest`, `achievements`,
  `project`, `exophase`, `psn`, `nadeo` and `ninjakiwi`. The old CLI `scan`, `stale` and `review`
  flows are gone, so don't go looking for them.
- The status vocabulary is split in two on purpose (see `internal/forms/forms.go`):
  `GameStatuses` (front matter, 10 values, including `backlog` and `software`) and
  `PlaythroughStatuses` (entries in `playthroughs.yaml`, 9 values, including `misc_launch`).
  `planned` is a third, placeholder-only entry status for planned replays. `software` is
  game-level only: it describes the whole record, not a mode a game can have alongside others.
  "Unplayed" is not a status at all. The site derives it (`layouts/partials/game-unplayed.html`)
  for any game with no playthrough entries and no captured playtime, and shows it as a tag.

---

## Outstanding

- **A few narrative games may still be one entry covering several runs.** The backfill wrote a
  single entry per game. Most of the flagged candidates have since been split: 48 games now have
  more than one playthrough, BG3 among them. Dishonored, A Hat in Time, and Phoenix Wright: Ace
  Attorney Trilogy are still single entries, and whether that's right is the owner's call. A gap in
  achievement unlocks marks an activity arc, not a playthrough, so boundaries must be confirmed by
  hand and never derived and applied. Splitting has a cost (see `SyncStatus` below).
- **A subset's parent lookup is memoized per process, not persisted.** `raParentCache` in
  `internal/commands/subsets.go` keeps Housekeeping from re-asking RetroAchievements (~1.2s a
  call) on every render, but each restart of `gamelog serve` pays for it again, once per
  unattached subset row. That's fine at today's count (one subset in the whole library). It will
  need a real cache well before the count grows much.
- **Nothing detaches a subset from the tool.** Attaching is a Housekeeping button. Undoing it
  means deleting the id from `retroachievements_subsets:` by hand. This is deliberate: the archive
  record is keyed by provider id and survives either way.
- **The Exophase canonical-ID map only covers the first 50 games per service.** The embedded
  `window.playerGames` payload isn't paginated the way the JSON API is. Only Ubisoft goes through
  Exophase now, with 2 games, so it isn't biting. Games past the ceiling are skipped, not
  mis-keyed.
- **Nintendo is on the Exophase profile but not archived.** Switch has no achievements (only
  playtime and last-played), so it needs a record shape that doesn't pretend otherwise, as
  Nadeo and Ninja Kiwi each got, before it's worth adding.
- **Three Hugo-side copies of the "last activity" rule.** `layouts/partials/games-timeline.html` and
  `game-year-rows.html` each fold across playthroughs and max against `achievement-summary.yaml`
  themselves. `game-first-played.html`/`game-last-played.html` are the shared version, used by
  `layouts/games/term.html`, `taxonomy-game-list.html` and `games-overview.html`. The copies are
  kept in step only by "same rule as game-last-played.html" comments. Consolidating them was
  deferred because the timeline and year rows need richer per-item data than the partials'
  bare-date return.

---

## Invariants: don't "fix" these

### Data safety

- **Achievement refreshes merge, never overwrite.** `mergeProvider` (`internal/model/archive.go`)
  keeps every unlock once recorded, and a fetch that errors or returns no achievements updates
  only the attempt metadata. The regression tests in `internal/model/archive_test.go` cover this.
  Keep them.
- **`Extra map[string]any` with `yaml:",inline"` is load-bearing** on `PlaythroughEntry`,
  `SessionEntry` and `FrontMatter`. It's the only thing keeping a hand-added field from being
  deleted by the next whole-file rewrite.
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
- **`SyncStatus` only acts when a game has exactly one entry.** With two or more, "which one does
  this status refer to?" is ambiguous, so it returns false. Splitting a game therefore opts it out
  of automatic status sync for good, and its entries become hand-maintained. That's deliberate,
  not a bug to route around.

### Status vocabulary

- **One-shot statuses gate per entry-status, per platform.** `forms.IsOneShot` (endless,
  multiplayer, software) is enforced by `mutate.OneShotConflict`: at most one entry of a given
  one-shot status per platform, while a *different*-status entry is a real second mode and is
  allowed. `mutate.EffectiveOneShotStatus` resolves older entries that say `playing` and lean on
  the game-level status for their meaning. Those don't need migrating.
- **Stale triage only looks at game-level `playing`** (`FindStaleCandidates` in
  `internal/commands/stale.go`), so endless/multiplayer/software games are never offered for
  finished/dropped triage, whatever their entries say.
- **Adding another one-shot status means changing four places together:** `forms.IsOneShot`,
  `PlaythroughStatuses` (only if it's a real per-entry mode), `NO_CONNECTOR_CLASSES` in
  `assets/js/games-timeline.ts`, and the status colour maps in `assets/scss/main.scss` and
  `_vars.scss`. The game-level override in `layouts/partials/games-timeline.html` also lists them.

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
  fetched once per process and cached (`raRecentlyPlayed` in the retroachievements package).
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

### Steam

- **Steam puts useful JSON in 400/401/403 bodies.** Read the body; don't bail on the status.
- **Steam needs a SteamID64**, not a vanity name. `SteamClient.resolveSteamID` handles both.

### Exophase (Ubisoft)

- **Exophase 403s Go's default User-Agent** (Cloudflare), so requests must send a browser one.
- **The JSON API omits `canonical_id`**, the ID the archive keys on. It exists only in the
  `window.playerGames` payload in the profile HTML, so the client joins the two on `master_id`. A
  game whose canonical ID can't be resolved is **skipped, not guessed at**.
- **Only the account page carries per-service player ids.** The per-service pages don't, and the
  per-service username differs from the account name.

### PSN (Sony's own trophy API)

- **The OAuth `authorize` step returns its code on a 302 to a non-resolvable app-scheme URI.** The
  client reads `code` off the `Location` header instead of following it
  (`http.ErrUseLastResponse`).
- **`npServiceName` must be `"trophy"` on every per-title call for PS3/PS4/Vita titles**, or the
  API answers as if the title doesn't exist. The client always sends what `trophyTitles` reported.
- **The npsso→token exchange is in-memory only, per client.** There is deliberately no refresh
  token cached across runs.
- The PSN records that predate this client were captured via Exophase. They were deleted and
  refetched, not merged: the two sources key trophies differently (slug vs numeric `trophyId`),
  and merging would have kept every trophy twice.

### Xbox 360 (OpenXBL)

- **Legacy 360 titles must use `/achievements/x360/{xuid}/title/{titleId}`.** The modern
  endpoint returns an empty list for them.
- **That endpoint only lists unlocked achievements**, so the total and last-played date come from
  `player/titleHistory`.

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

- **`last_played` from `achievement-summary.yaml` is maxed against logged playthroughs** in
  `game-last-played.html`, `games-timeline.html` and `game-year-rows.html`. That's intentional: a
  refreshed archive can legitimately be more recent than any logged session. Don't make it
  agree with `playthroughs.yaml`.

---

## History

Kept only because the invariants above point back to these. The details are in git history.

- **Front-matter line-splicing destroyed or misattributed fields.** Guessed YAML field boundaries
  deleted or re-parented data while producing valid YAML and exiting 0. Retired by moving
  playthroughs to a whole-file `playthroughs.yaml`, which is where the loss check, `Extra` and
  string dates come from. `TestAddSession_ConvertsWithoutDisturbingSiblings` and
  `TestAddSession_LeavesTheEarlierSessionAlone` carry the original repros.
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
  been used on most of the flagged games. What's left is listed under Outstanding.
