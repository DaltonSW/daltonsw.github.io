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
  | session-based | 41 |
  | dropped | 21 |
  | multiplayer | 16 |
  | playing | 4 |
  | paused | 2 |
  | software | 1 |

  `backlog` is the only `gameStatuses` value now unused. The four `playing` entries are genuinely
  active — anything quiet for 30+ days has been triaged.
- **The vocabulary is split in two, on purpose:** `gameStatuses` (front matter, 9 values) and
  `playthroughStatuses` (entries in `playthroughs.yaml`, 5 values). `session-based`,
  `multiplayer`, and `software` are game-level only — those entries keep a single playthrough
  whose status stays `playing` while its `sessions:` list accumulates, which is what `isOneShot`
  in `forms.go` gates on. Two consequences worth knowing before touching either list:
  - `gamelog stale` only considers `status == "playing"`, so all three are excluded from
    finished/dropped triage automatically. That's correct: none of them has a finish line.
  - `layouts/partials/games-timeline.html` colours bars from the **entry** status, which for these
    three is always `playing`. It special-cases them and lets the game-level status win, or an
    endless sandbox would render identically to an active playthrough. **Adding a fourth
    one-shot status means updating that list too** — `isOneShot`, the timeline's slice, and the
    `.pill--`/`.entry--`/`.pt--` rules in `main.scss` all have to move together.
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
consecutive achievement unlocks. Roughly half are already `session-based`, where the `sessions:`
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
  purpose (`parseRAAwardDate` vs `parseRADate`).
- **An unknown RA game ID returns HTTP 200 with `[]`**, not a 404.
- **RA's `UserTotalPlaytime` (added to `GetGameInfoAndUserProgress` in late 2025) is in seconds**,
  unlike everything else in the archive which stores minutes — `FetchRARecord` divides by 60.
  Client-tracked (RetroArch/RAIntegration), so games played before that rollout, or on an
  unsupported emulator, legitimately have none.
- **Steam puts useful JSON in 400/403 bodies** — read the body, don't bail on status.
- **Steam needs a SteamID64**, not a vanity name; `resolveSteamID` handles both.
- **RA rate-limits sustained bursts with 429** — `RAClient` throttles to ~1.2s and retries.
- **Achievement refreshes merge, never overwrite.** An unlock recorded once must never be
  retractable. Regression tests in `achievements_test.go` cover this; keep them.
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
- **Exophase's JSON API omits `canonical_id`** — the real `NPWR…` PlayStation ID the archive keys
  on. It exists only in the `window.playerGames` payload embedded in the profile HTML, so the
  client reads both and joins on `master_id`. A game whose canonical ID can't be resolved is
  **skipped, not guessed at**: mis-keying the archive is the exact failure the ID scheme prevents.
- **Only the account page carries per-service player ids.** `/psn/user/<name>/` does not, and the
  per-service username differs from the account name (Nintendo's is an opaque hash).

## Outstanding

- **The Exophase canonical-ID map only covers the first 50 games per service.** The embedded
  profile payload isn't paginated the way the JSON API is, so a service with more than 50 games
  would silently resolve IDs for only the first page. PSN currently has 13, so this isn't biting
  yet — but it will for Steam-sized libraries, and games beyond the ceiling are skipped rather
  than mis-keyed.
- **Nintendo and Ubisoft are captured on the Exophase profile but not archived.** Switch has no
  achievements at all (playtime and last-played only), so it needs a record shape that doesn't
  pretend otherwise before `providerNintendo` is worth adding.

`tools/gamelog/README.md` documents all of the above in more detail.
