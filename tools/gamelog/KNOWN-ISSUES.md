# gamelog — known issues

Outstanding problems in `tools/gamelog/`, written for someone picking this up fresh.

Read `CLAUDE.md` at the repo root first — it states the project intent these issues are judged
against. The short version: **this is meant to be a durable archive of gameplay history, and
losing captured data is the worst thing that can happen.**

## Current state

- `content/games/` holds only the section index (`_index.md`). The sample entries that used to
  live there were fake test data and have been deleted.
- Storage is split three ways: hand-authored front matter in `_index.md`, tool-owned
  `playthroughs.yaml` beside it, and captured API history in `archive/<provider>/<id>.json`
  outside `content/` entirely. `README.md` explains why each is where it is.
- A review list of **211 unlogged games** (45 RetroAchievements, 166 Steam ≥5h) was generated and
  is with the owner for triage. Creating them is pending their picks.
- `gamelog scan` finds them; `gamelog achievements <slug>` captures per-game unlock history.
  Both work and are tested.

---

## 6. Nothing renders the archive

`gamelog achievements` has always captured full per-achievement history, and no template has
ever read it. The site's timeline is still driven entirely by hand-typed playthrough dates, so
the richest data in the repo is invisible.

This is now a clean piece of work rather than a tangled one, which is the point of the split: the
archive is not reachable from templates, so displaying it means adding a **projection** step —
`gamelog` generating a slim, `raw`-free file into each game bundle for Hugo to read. Iterating on
the display shape then never touches captured data and never re-fetches.

Two things to know before starting:

- **Hugo copies bundle resources to the built site.** That is why `playthroughs.yaml` is covered
  by `_build.publishResources: false`, cascaded from `content/games/_index.md`. A projection file
  needs the same treatment or it gets served publicly.
- The obvious first consumer is `layouts/partials/games-timeline.html`, which currently derives
  everything from `playthroughs`/`sessions`. Real unlock dates are a far better signal for a game
  played across years (Spelunky's run 2013 → 2020 with multi-year gaps), but it is a reshape of
  the main existing consumer — worth doing separately from wiring the data in.

---

## 7. Open question: archive size at full backfill

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

## Resolved

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

`tools/gamelog/README.md` documents all of the above in more detail.
