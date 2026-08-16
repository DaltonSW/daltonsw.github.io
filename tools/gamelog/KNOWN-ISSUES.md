# gamelog — known issues

Outstanding problems in `tools/gamelog/`, written for someone picking this up fresh.

Read `CLAUDE.md` at the repo root first — it states the project intent these issues are judged
against. The short version: **this is meant to be a durable archive of gameplay history, and
losing captured data is the worst thing that can happen.**

## Current state

- The backfill is **done**. `content/games/` holds **213 game bundles**, each with an `_index.md`
  and an `achievement-summary.yaml`; **195** also have a `playthroughs.yaml`. The 18 without one
  are legal, not broken — `LoadPlaythroughs` returns an empty struct for a missing file, and a
  game with no logged playthrough is a real state.
- Storage is split three ways: hand-authored front matter in `_index.md`, tool-owned
  `playthroughs.yaml` beside it, and captured API history in `archive/<provider>/<id>.json`
  outside `content/` entirely. `README.md` explains why each is where it is.
- `archive/` holds **213 records — 168 Steam, 45 RetroAchievements, 6.9 MB on disk** including
  every verbatim `raw` response. One record per game.
- `gamelog scan` finds unlogged games; `gamelog achievements <slug>` captures per-game unlock
  history; `gamelog project` regenerates the summaries from disk without API calls. All tested.
- **Every one of the 213 games is `draft: true`** — the game log is fully captured but not
  published. Publishing is a separate decision from capture, and `gamelog review` is the tool for
  walking the drafts.
- Game-level status distribution, after the reclassification and dropped/finished triage passes:

  | status | count |
  |---|---|
  | finished | 81 |
  | mastered | 47 |
  | endless | 41 |
  | dropped | 21 |
  | multiplayer | 16 |
  | playing | 4 |
  | paused | 2 |
  | software | 1 |

  `backlog` is unused in the counts above but no longer unwritable: Housekeeping's Backlog
  section creates entries with it from the owned-but-unplayed half of the Steam library. Every
  `gameStatuses` value now has something that produces it. The four `playing` entries are
  genuinely active — anything quiet for 30+ days has been triaged.
- **The vocabulary is split in two, on purpose:** `gameStatuses` (front matter, 9 values) and
  `playthroughStatuses` (entries in `playthroughs.yaml`, now 7 values — `endless` and
  `multiplayer` joined the original 5 so a game with genuinely distinct modes, e.g. Hitman's
  story campaign, its Freelancer roguelike, and its multiplayer contracts, can have one entry per
  mode instead of the game being forced into a single label). `software` stays game-level only —
  it describes the whole record ("this isn't a game"), not a mode a game can have alongside
  others, so there's no per-entry equivalent.
  - Every `endless`/`multiplayer` game written before this still has its sole entry's own
    `status` as `playing`, relying entirely on the game-level status for its real meaning — that
    convention still works and doesn't need migrating. `mutate.EffectiveOneShotStatus`
    resolves it so the one-shot guard still recognizes those old-style entries.
  - `forms.IsOneShot` now gates **per entry-status, per platform**, not per game
    (`mutate.OneShotConflict`): at most one entry of a given one-shot status per platform,
    since none of the three has a save file or finish line and a second one on the same platform
    would be fragmentation — but a *different*-status entry (a `mastered` campaign next to an
    `endless` sandbox mode) is a real second mode, not fragmentation, and is allowed.
  - `gamelog stale` only considers `status == "playing"` **at the game level**, so a game whose
    front-matter status is endless/multiplayer/software is still excluded from finished/dropped
    triage regardless of what any individual entry's status says. Correct either way: the
    game-level status is what `stale` and `SyncStatus` both key off, and neither was changed here.
  - `layouts/partials/games-timeline.html` colours bars from the **entry** status directly, and
    needed no change: its game-level override (forcing every entry to the game's own status) only
    ever fires when the *game's* front-matter status is endless/multiplayer/software, so a mixed
    game just needs its front-matter status set to something else (its "headline" mode) for
    per-entry endless/multiplayer to render as authored. **Adding a fourth one-shot status still
    means updating `isOneShot`, `playthroughStatuses` (only if it's a real per-entry mode),
    `games-timeline.ts`'s `NO_CONNECTOR_CLASSES`, and the `.pill--`/`.entry--`/`.pt--` rules in
    `main.scss` together.**
- `software` marks an entry that isn't a game at all (currently just Aseprite). Steam sells tools
  alongside games and reports playtime for them identically, so they arrive through the same
  `gamelog scan`; the status says the completion vocabulary doesn't apply rather than forcing a
  finished/dropped answer to a question that was never asked.

---

## 6. Open question: archive size at full backfill

Each record keeps the verbatim API response under `raw`, so no future field needs a re-fetch.
The owner has confirmed keeping it in full.

Measured on two real records (Hades on both services): ~7.6 KB of `raw` for a 49-achievement
Steam game, and the pair of records totalling 40 KB on disk. The ~5.6 MB figure previously quoted
here for the full 211-game backfill was an extrapolation from a different data shape and should
not be trusted — **measure it during the backfill instead of restating it.**

Whatever the total, the cost is now repo size only. The archive sits outside the Hugo tree, so it
is never parsed at build time and never published. Revisit only if the owner asks; dropping `raw`
for Steam alone would recover the most with the least loss, since Steam's response carries fewer
unnormalised fields than RA's.

---

## 7. Open: every game has exactly one playthrough entry

The backfill wrote a single `playthroughs.yaml` entry per game, so a game played through more
than once is indistinguishable from one long stretch of play. Baldur's Gate 3 carries 431 hours
and 45/54 achievements in one entry spanning 2023-10 → 2025-03; the owner confirms that is at
least two separate campaigns.

Nothing is broken. The schema already supports this — `playthroughs:` is a list, and both
`AddSession` and "Start a new playthrough" exist. The historical data was simply never split.

**Signal available for proposing splits:** 52 of 213 archived games show a 6+ month gap between
consecutive achievement unlocks. Roughly half are already `endless`, where the `sessions:`
list is the correct representation and a split would be actively wrong. The real candidates are
the narrative games — BG3, Dishonored, Celeste, DARK SOULS: REMASTERED, Fallout: New Vegas,
Portal 2, Cuphead, Phoenix Wright, A Hat in Time, Super Meat Boy, Shovel Knight (~12 in total).

**A gap in unlocks marks an activity arc, not a playthrough.** Resuming a three-year-old save
looks identical to starting a fresh run. Boundaries have to be confirmed by the owner, never
derived and applied.

**Trade to weigh before splitting anything:** `SyncStatus` only acts when there is exactly one
entry — `len(f.Playthroughs) != 1` returns false, because two entries make "which one does this
status refer to?" ambiguous. Splitting a game therefore opts it out of automatic status sync
permanently; `gamelog stale` and the front-matter status path stop keeping its entries in step
and it becomes hand-maintained. That is deliberate, so it's a real cost, not a bug to route around.

Related and currently unused: `notes:` and `rating:` exist on every entry and are set by zero
games. `notes:` is the natural home for context the numbers can't carry — that Hollow Knight,
FEZ, and Dishonored's high playtimes are speedrun practice rather than story progress, for
instance, which is exactly the kind of fact that made playtime misleading during status triage.

Deferred by the owner until the status reclassification lands.

---

## Resolved

### 6 — nothing rendered the archive

`gamelog achievements` had always captured full per-achievement history, and no template read it.
Resolved with the projection step this issue anticipated: `gamelog project` sums each game's
`unlocked`/`total` across every provider it's linked to and writes the result to
`content/games/<slug>/achievement-summary.yaml` — no `raw`, no per-achievement list, just the two
numbers. `gamelog scan`/`gamelog achievements` call the same write automatically; `project` itself
makes no API calls, so it's free to re-run for a bulk backfill or after the summary's shape
changes. Covered by the same `_build.publishResources: false` cascade as `playthroughs.yaml`, so
it's readable via `.Resources.Get` at build time but never published.

The first consumer is the games list (`layouts/games/taxonomy.html`), which now shows the count
on the right side of each row, next to the date/rating. `layouts/partials/games-timeline.html`
(the vis-timeline visualisation) still derives everything from `playthroughs`/`sessions` —
reshaping it to use real unlock dates (Spelunky's run 2013 → 2020 with multi-year gaps is a much
better signal than a hand-typed date range) is still open, and is a separate piece of work from
wiring the data in at all.

### 4 — two storage formats, no archive/presentation split

The archive lived at `content/games/<slug>/achievements.json`, keyed by the game's slug. A slug
is a URL — a presentation decision — so renaming a game moved the archive and deleting a game
destroyed captured history. Front matter meanwhile held `playthroughs`/`sessions` as a structured
list maintained by line splicing.

Split three ways. The archive moved to `archive/<provider>/<id>.json`, keyed by IDs that never
change and stored outside `content/`; `LoadRecord`/`SaveRecord` replaced the per-game
`AchievementArchive` wrapper, while `mergeProvider`/`mergeAchievement`/`summarize` were left
untouched. Playthroughs moved to a tool-owned `playthroughs.yaml` encoded whole, which retired
`patch.go` entirely. Front matter is now read-only to the tool after creation.

**Found while verifying:** Hugo copies page-bundle resources into the built site unconditionally,
so the old layout was publishing every captured API response — `raw` included — to the public
site. Moving the archive out of `content/` fixed that; `playthroughs.yaml` is kept unpublished by
a `_build.publishResources: false` cascade in `content/games/_index.md`.

Rendered output was verified byte-identical against a `HEAD` build of the same content, with an
identical published file set.

### 5 — `raw` response storage size

Resolved as a decision, not a code change: keep `raw` in full. See issue 7 above for what
replaced the open question.

### 1 & 2 — front-matter edits destroyed or misattributed fields

Both came from the same root cause: the line-splice editor guessed where a YAML field ended
instead of computing it. `ConvertToSessionsEdit` replaced the whole span from `started:` to
`finished:`, deleting anything written between them; `SessionsEndLine` assumed every session was
exactly two lines, so a trailing field on the last session was adopted by a newly appended one.
Both produced valid YAML and exited 0.

First fixed in place with a real boundary walk (`Doc.FieldRange`), then removed outright by issue
4: with `playthroughs.yaml` encoded whole, the conversion is a slice assignment and there is no
line arithmetic left to get wrong. `TestAddSession_ConvertsWithoutDisturbingSiblings` and
`TestAddSession_LeavesTheEarlierSessionAlone` carry both original repros forward.

### 3 — slug generation was lossy and could target the wrong file

`Slugify` lowercased then replaced `[^a-z0-9]+`, so `Ōkami` became `kami` and `Pokémon Red` became
`pok-mon-red`. `!@#$%` produced an empty slug, which resolves
`filepath.Join(gamesDir, "", "_index.md")` onto `content/games/_index.md` — the section index
page — blocked in the real repo only because that file happened to exist.

Now folds accents through NFD before stripping (`okami`, `pokemon-red`), spells out the letters
that have no decomposed form (`ß→ss`, `æ→ae`, …), and `validateSlug` rejects empty slugs and
anything containing a path separator or `..` from inside `CreateGameFile`, where it can't be
bypassed. A slug collision now names the game already holding it.

Worth knowing: this was **not** urgent. Run against the real 211-game corpus, the old `Slugify`
produced zero empty slugs, zero collisions, and correct output for every title — the only
non-ASCII characters in the library are `™` and `®`, which already stripped correctly. It was
fixed because slugs become permanent URLs. `TestSlugify` pins the `™`/`®` cases so that stays true.

---

## Things that are already handled — don't "fix" these

- **RetroAchievements 403s Go's default User-Agent.** `raUserAgent` must stay set on every
  request. `curl` does not reproduce the failure.
- **`a=1` is required** on `API_GetGameInfoAndUserProgress.php` or award fields come back null.
- **Award dates are RFC3339; per-achievement dates are zoneless SQL datetimes.** Two parsers on
  purpose (`retroachievements.ParseRAAwardDate` vs `retroachievements.ParseRADate`).
- **An unknown RA game ID returns HTTP 200 with `[]`**, not a 404.
- **RA's `UserTotalPlaytime` (added to `GetGameInfoAndUserProgress` in late 2025) is in seconds**,
  unlike everything else in the archive which stores minutes — `retroachievements.FetchRecord`
  divides by 60. Client-tracked (RetroArch/RAIntegration), so games played before that rollout, or
  on an unsupported emulator, legitimately have none.
- **An RA record's `last_played` comes from a second endpoint on purpose.**
  `GetGameInfoAndUserProgress` has no last-played field at all, so `last_played` is filled from
  `API_GetUserRecentlyPlayedGames`, fetched once per process and cached by `raRecentlyPlayed` in
  `internal/commands/achievements.go`. Don't collapse it back into the per-game fetch (RA's 1.2s
  throttle makes that ~2 extra requests × every game), and don't drop it as redundant with the
  newest unlock date — that equivalence is exactly the bug it fixed: a game played for weeks
  without unlocking anything read as last played on the day of its last achievement. Its `c`
  parameter is capped at 50 server-side, so the paging loop is required, not defensive.
- **Steam puts useful JSON in 400/403 bodies** — read the body, don't bail on status.
- **Steam needs a SteamID64**, not a vanity name; `SteamClient.resolveSteamID` handles both.
- **RA rate-limits sustained bursts with 429** — `RAClient` throttles to ~1.2s and retries.
- **A RetroAchievements subset is linked by `ParentGameID`, never by its title.** The
  `<base> [Subset - <name>]` convention is the only signal the bulk scan endpoint carries, and
  it's used to *spot* a candidate, nothing more; `commands.AttachSubset` re-fetches the per-game
  endpoint and refuses to write unless the reported parent matches the target game's own
  `retroachievements_id`. Don't "simplify" that away — a form value that attaches another game's
  achievements to this game would look entirely plausible on the rendered page. See
  `internal/commands/subsets.go` and README.md's "RetroAchievements subsets".
- **Subset counts are deliberately kept out of a game's headline `unlocked`/`total`.** They live
  under `subsets:` in `achievement-summary.yaml` instead, because a subset is a bonus set for the
  same game rather than a second copy of it: 16/40 + 6/52 = 22/92 describes a set that exists
  nowhere and restates a 40%-complete game as 24% complete. Subset *playtime* is left out for a
  related reason — RA reports it per game id, so summing double-counts the same hours — while
  `last_played` **is** folded in, since playing the bonus set is playing the game.
- **`SaveAchievements` walks links in order rather than one id per provider.** A subset is a
  second RetroAchievements id on one game, which is the only case in the schema where a game has
  two ids on one provider; collapsing links back into a `map[provider]id` would silently drop
  every subset but the last. It also passes a **blank title** for a subset record so a refresh
  can't overwrite RA's own name for the set with the game's — that name is the only place
  "Mouse Alley" is recorded.
- **Achievement refreshes merge, never overwrite.** An unlock recorded once must never be
  retractable. Regression tests in `internal/model/archive_test.go` cover this; keep them.
- **`Extra map[string]any` with `yaml:",inline"` is load-bearing**, on both `PlaythroughEntry`
  and `SessionEntry`. It is the only thing keeping a hand-added field from being deleted by the
  next whole-file rewrite. Removing it produces exactly the class of silent loss issues 1 and 2
  were about.
- **Dates in `playthroughs.yaml` are typed as `string` on purpose.** YAML resolves an unquoted
  `2026-01-04` to a timestamp; decoding that into `any` yields a `time.Time` that re-encodes as
  RFC3339, rewriting every date in the file.
- **`confirmAndWrite` diffs the pending rewrite against the bytes on disk and refuses to write if
  a field would vanish.** Operations that legitimately remove a path declare it. If a new
  operation trips this check, the operation is wrong; don't widen the allowlist to silence it.
- **Playthrough writes are atomic** (temp file + rename). It's a whole-file rewrite of the only
  copy of that data.
- **The archive is keyed by provider ID, not slug, and lives outside `content/`.** Both halves
  matter: slugs change, and anything inside `content/` gets published.
- **Exophase 403s Go's default User-Agent** (Cloudflare). Requests must send a browser one.
- **Exophase's JSON API omits `canonical_id`** — the real ID the archive keys on. It exists only
  in the `window.playerGames` payload embedded in the profile HTML, so the client reads both and
  joins on `master_id`. A game whose canonical ID can't be resolved is **skipped, not guessed
  at**: mis-keying the archive is the exact failure the ID scheme prevents.
- **Only the account page carries per-service player ids.** `/uplay/user/<name>/` does not, and
  the per-service username differs from the account name (Nintendo's is an opaque hash).
- **PSN moved off Exophase to Sony's own trophy API** (`internal/providers/psn`) — see
  `README.md`'s "PlayStation, via Sony's own trophy API". Quirks specific to it:
  - The OAuth `authorize` step returns its code as a query parameter on a **302 redirect to a
    non-resolvable app-scheme URI**; the client must read `code` off the `Location` header rather
    than follow the redirect (`http.ErrUseLastResponse` in `PSNClient.authorize`).
  - `npServiceName` **must** be `"trophy"` on every per-title call for a PS3/PS4/Vita title, or
    the API responds as though the title doesn't exist. `"trophy2"` (PS5/PC) titles work either
    way. The client always sends whatever `trophyTitles` reported for that title, so this doesn't
    need separate handling per call site.
  - The npsso→access-token exchange happens once per `PSNClient`, in-memory, never persisted.
    That's deliberate: the access token outlives any single CLI run, and there is no
    machinery anywhere else in this tool for caching a refresh token across runs — don't add it
    unless a real need (a long-running daemon, say) shows up.
  - The 12 archive files that predate this (`archive/psn/*.json`, captured via Exophase) were
    deleted and refetched rather than merged in place: Exophase's achievement `Key` was its own
    slug, Sony's is a numeric `trophyId`, and the archive merges achievements by `Key` — merging
    the two sources in place would have kept every trophy twice, once under each key scheme,
    permanently doubling `unlocked`/`total`. Safe to do since Exophase was already relaying Sony's
    own unlock timestamps verbatim; nothing was lost.
- **Nothing removes an entry from `playthroughs[]` by index.** `SplitPlaythrough` always appends
  its new entry at the true end of the slice specifically so no existing entry's index — and
  therefore no path into a later entry's nested `sessions[]` — ever moves. A delete-by-index
  operation would shift everything after it, and `removeSessionAllowedPaths`'s pairwise-diff
  approach only ever compared flat per-session scalar fields; it doesn't generalize to a shifted
  entry's own nested sessions changing path identity. `GraduatePlannedPlaythrough` sidesteps this
  by mutating in place instead of replacing an entry, and the "planned replay" feature has no
  delete path at all — removing a stale `status: planned` placeholder is left to hand-editing
  `playthroughs.yaml`, which the file's own generated banner already sanctions.
- **Trackmania/Nadeo is deliberately outside the achievement path.** `ProviderNadeo` is *not* in
  `ProviderOrder` and *not* produced by `BuildProviderLinks`, and `nadeo_account_id` has its own
  accessor rather than joining `Doc.ProviderLinks()`. That isn't an oversight to tidy up: a Nadeo
  record holds lap times, and everything walking `ProviderLinks` sums an `unlocked`/`total` pair.
  Adding it would fold ~450 campaign tracks into Trackmania's achievement denominator and restate
  a 21/30 game as 433/480. This is the same call `Outstanding` records for Nintendo — a provider
  whose data isn't achievements needs a record shape that doesn't pretend otherwise.
- **A Trackmania personal best merges by *minimum*, which is inverted from every other numeric
  field in the archive.** `mergeProvider` takes the larger of two values (`if fresh.Total >
  merged.Total`) because more achievements is more history; a lap time is better when it's
  smaller. `mergeNadeoTrack` therefore keeps the faster of old and fresh, and keeps the archived
  one when a fetch reports no time at all. Taking the max here would silently replace every record
  with the worst time ever seen. `WorldRank` moves only when the time does, since a rank belongs
  to the run that set it. Regression tests in `internal/model/nadeo_test.go` cover this; keep them.
- **A `nadeo fetch --season` covers one campaign, and the merge is the only thing making that
  safe.** `mergeNadeoRecord` unions campaigns by `SeasonUID` and tracks by `MapUID`, so a
  single-season fetch leaves the other seventeen untouched rather than replacing them — the same
  property that makes a rate-limited or partial response harmless. Without the union, `--season`
  would be a data-loss bug rather than a convenience.
- **`fetch` defaults to the current season, and `nadeoMinInterval` is 500ms, because the
  credentials belong to a real player.** A Nadeo service account inherits the Ubisoft account it
  was created on — which is *why* the personal-records endpoint returns the right person's times,
  and also why a rate-limit violation lands on an account someone plays games on. `--all` is ~40
  requests and is meant to be a deliberate, rare choice.
- **Trackmania times are captured at two scopes, and neither is redundant.** `season_best_ms` is
  what was achieved while a campaign was open (frozen when it closes); `personal_best_ms` is the
  all-time best, which moves whenever an old track is revisited to clean up a medal. The grid
  shows the all-time medal and marks the cells that only got there afterwards. They are two
  requests because `mapIdList` and `seasonIdList` both filter every result and intersect if sent
  together — and **the response must be filtered on `scopeType` regardless**, since a `mapIdList`
  request has no season filter and returns season-scoped entries alongside the all-time ones.
  Don't "simplify" that to one call.
- **Trackmania records have two fetch paths on purpose, and they report different halves of a
  run.** The Core `/v2/accounts/{id}/mapRecords` endpoint carries the run's `timestamp`, `medal`
  and replay but no zone rankings; the Live `leaderboard/group/map` fallback knows the rank but no
  date. `mergeNadeoTrack` therefore treats an *identical* time as the same lap seen twice and
  backfills the missing half, while an *improved* time replaces every run-scoped field wholesale —
  blanks included, because attributing the superseded lap's date or replay to a new record would
  be a false statement rather than preserved history. This is the one place in the archive where
  clearing a field is correct.
- **A `recordScore.time` of `4294967295` is `math.MaxUint32`, not a 49-day lap.** It's the
  sentinel for a map with a secret threshold score. `AccountRecord.Usable` filters it; recording
  it would put an absurd time in the archive that the minimum-wins merge could never displace.
- **Nadeo batch responses are matched by `mapUid`+`groupUid`, never by position.** An invalid or
  unknown pair is *omitted* from the response rather than returned empty, so responses aren't 1:1
  with requests; zipping them would assign one track's time to another. The 50-map cap on that
  endpoint is also a silent truncation, not an error.
- **The Nadeo leaderboard endpoint intermittently returns `[]` for valid parameters.**
  `recordBatch` retries once before concluding a batch has no times. Two empty responses in a row
  are taken at face value — there's no unbounded retry.
- **A Nadeo record's `raw` reflects the most recent fetch's scope, not the whole archive**, and is
  replaced wholesale — the same rule `mergeProvider` applies to `ArchiveRecord.Raw`. It is stored
  once per record rather than per campaign on purpose: map metadata and records are batched
  *across* season boundaries, so no verbatim slice of a response belongs to a single campaign, and
  attaching each batch to every campaign it touched stored the same ~500KB payload once per season
  (25× on a full backfill — this was a real bug, caught by the file size). Nothing decoded is lost
  when a `--season` run narrows `raw`; the merge guarantees that separately, and `raw` is only a
  safety net for a field this tool doesn't model yet.
- **Nadeo tokens are held in memory only, per audience, and never persisted** — the same standing
  decision as PSN's npsso exchange. The two audiences (`NadeoServices` for Core, `NadeoLiveServices`
  for Live) are separate tokens and are not interchangeable.
- **`achievement-summary.yaml`'s `last_played` being maxed against logged playthroughs** in
  `layouts/partials/game-last-played.html`/`games-timeline.html`/`game-year-rows.html` is
  intentional, not drift. A refreshed archive can legitimately be more recent than a logged
  session (see `README.md`'s notes on the two staying independently useful). Don't "fix" it into
  agreeing with `playthroughs.yaml` — that would hide real, more-recent activity.

## Outstanding

- **Only Summer 2020 is archived so far**; the other 24 official campaigns need
  `gamelog nadeo fetch --all` (~15 requests). Everything the grids render is already driven by
  whatever is in the archive, so this is a data gap rather than a code one.
- **A subset's parent lookup is memoized per process, not persisted.** `raParentCache` in
  `internal/commands/subsets.go` keeps Housekeeping from re-asking RetroAchievements (~1.2s a
  call) on every render, but a restart of `gamelog serve` pays for it again — one request per
  unattached subset row. That's fine at today's count (one subset in the whole library) and would
  want a real cache well before it wasn't.
- **Nothing detaches a subset from the tool.** Attaching is a Housekeeping button; undoing it
  means deleting the id from `retroachievements_subsets:` by hand. Deliberate for now, and the
  same call made for planned-replay placeholders: the archive record is keyed by provider id and
  survives either way, so nothing is lost by the edit being manual.
- **The Exophase canonical-ID map only covers the first 50 games per service.** The embedded
  profile payload isn't paginated the way the JSON API is, so a service with more than 50 games
  would silently resolve IDs for only the first page. Now Ubisoft-only (PSN moved to Sony's own
  API, which paginates properly): Ubisoft currently has well under 50 games, so this isn't biting
  yet, and games beyond the ceiling are skipped rather than mis-keyed if it ever does.
- **Nintendo is captured on the Exophase profile but not archived.** Switch has no achievements
  at all (playtime and last-played only), so it needs a record shape that doesn't pretend
  otherwise before `providerNintendo` is worth adding. Ubisoft was in the same boat and has since
  been archived the same way PSN briefly was (`providerUbisoft`, `ubisoft_id`,
  `exophase.FetchUbisoftRecord`, Exophase's `uplay` environment) — see README.md's "Ubisoft, via
  Exophase".
- **`games-timeline.html` and `game-year-rows.html` each independently reimplement the same
  "fold across playthroughs, max against achievement-summary" algorithm** that
  `layouts/partials/game-first-played.html`/`game-last-played.html` also implement (now wired into
  `layouts/games/term.html`/`single.html` — see the fix for the drift between a game's own page
  and everywhere else it's listed). That's three Hugo-side copies of one algorithm, kept in sync
  only by comment cross-reference ("mirrors game-last-played.html"), not shared code. Deliberately
  deferred: those two views need much richer per-item data (colors, statuses, year-clipped spans)
  than the two partials' bare-string return, so consolidating them is a bigger refactor than a
  template swap — not forgotten, just scoped out.

`tools/gamelog/README.md` documents all of the above in more detail.
