# gamelog

A CLI + local web UI for maintaining this site's `content/games/<slug>/_index.md` playthrough log.

Run with no arguments (or `gamelog serve`) to serve the local web UI over `content/games`: pick or
create a game, edit its info, log a new playthrough or session, update an existing one, or run
housekeeping (scan for unlogged games, flag stale ones, bulk-close open playthroughs, refresh
achievements). Everything that touches the log is done there now — the rest of the CLI is a
handful of read-only/scriptable commands (see `gamelog help`).

```
go run ./cmd/gamelog          # serve the web UI (default port 8080)
go run ./cmd/gamelog help     # usage summary
```

## Code layout

Standard `cmd/` + `internal/` Go layout. `cmd/main.go` is just command dispatch (spf13/cobra);
everything else lives under `internal/`, one package per concern:

```
internal/model         domain types + storage: front matter, playthroughs.yaml, the archive
internal/forms         the shared validation/status vocabulary, plus the couple of huh prompts
                        gamelog suggest and gamelog achievements still use
internal/mutate        confirm-preview-and-write-with-loss-check, shared by the web server and
                        the achievements command's own-session nudge
internal/providers/*   one package per API client (steam, retroachievements, exophase, xbox)
internal/dotenv        the tool's own minimal .env reader
internal/externalid    parses a pasted RA/Steam ID or URL into a bare ID
internal/commands      suggest/achievements/project, plus the scan/stale/close library
                        functions internal/server calls (see the package doc comments)
internal/server        the local web UI (`gamelog serve`, also what bare `gamelog` runs)
```

`server` is the maintained frontend over the `model`/`forms`/`mutate` layer. `commands` used to
also hold interactive CLI walkthroughs for scan/stale/close/review; those were sunset once the
web UI covered the same ground, but their underlying report/apply functions stayed — `server`
still imports and calls them directly.

## Where things live

Three layers, deliberately separate, because they have different owners and different rules:

```
content/games/<slug>/
  _index.md          authored by hand. The tool writes it at creation, and can edit
                     select fields afterwards (see "How front matter is edited")
                     without ever touching the markdown body below it.
  playthroughs.yaml  tool-owned. Encoded whole on every write.

archive/
  retroachievements/4650.json      captured from the API. Merge-on-write, never
  steam/1145360.json               hand-edited, never read by Hugo.
  psn/NPWR16532_00.json            PlayStation, mirrored via Exophase (see below).
  ubisoft/12345.json                Ubisoft Connect, mirrored via Exophase too.
  xbox/1480657033.json              Xbox 360, via OpenXBL (see below).
  ignored.yaml                     games you've decided never to log, so Scan and
                                   Backlog stop offering them. Rewritten whole.
```

**The archive is keyed by provider ID, not by slug, and lives outside `content/`.** A slug is a
presentation decision — it's the URL. When the archive was stored under one, renaming a game
moved it and deleting a game destroyed unlock history that may be impossible to re-fetch. Provider
IDs never change. Being outside the content tree also means Hugo never parses `raw`, and never
publishes it: bundle resources are copied to the built site, so the old layout was serving every
captured API response publicly.

A game on several services gets one archive file each. Nothing in the archive links them —
`retroachievements_id`, `steam_appid`, `psn_id`, `ubisoft_id` and `xbox_id` in front matter do, which is the
right place for a judgement a human made rather than something any provider reported. The same
holds for `retroachievements_subsets`, which is how one game ends up with more than one file
under `retroachievements/` — see "RetroAchievements subsets" below.

## How playthroughs are written

`playthroughs.yaml` is decoded, modified as a struct, and re-encoded whole. That's a deliberate
replacement for the line-splice editor this tool used to patch front matter with, which computed
line ranges from a `yaml.Node` tree and produced the two worst bugs in the project's history —
one deleted every field written between `started:` and `finished:`, the other reattached a
trailing field to the wrong session. Both wrote valid YAML and exited 0. Converting a playthrough
to `sessions:` is now a slice assignment, so neither failure has anywhere left to live.

Whole-file encoding has its own way to lose data: the encoder drops any key the struct doesn't
model. Three things prevent that, and all three matter:

- **`Extra map[string]any` with `yaml:",inline"`** on both the entry and session structs captures
  unrecognised keys and re-emits them. Without it, a hand-added `mood: obsessive` disappears on
  the next write.
- **`mutate.ConfirmAndWrite` re-encodes the pending result and diffs it against the bytes on disk.**
  Anything present beforehand that would vanish aborts the write:

  ```
  error: refusing to write — this edit would remove
    playthroughs[0].rating: "7"
    playthroughs[0].status: "playing"
  ```

  An operation that legitimately removes a path declares it — converting to `sessions:` moves the
  flat date pair, and clearing a field drops its key. Anything else tripping this check means the
  operation is wrong; fix it rather than widening the allowlist.
- **Writes are atomic** (temp file + rename). This is the only copy of the playthrough data and
  it's rewritten whole, so a crash partway through must not be able to truncate it.

Two encoding details worth knowing before changing the structs. Dates are typed as `string`
explicitly: YAML resolves an unquoted `2026-01-04` to a timestamp, and letting that become a
`time.Time` would rewrite every date in the file on the next save. `finished` on a session is
*not* `omitempty`, so an ongoing session keeps the key rather than having the field vanish.

**Comments in `playthroughs.yaml` are not preserved** — the file says so in a banner it re-emits
on every write. Prose belongs in `_index.md`'s markdown body, which a front-matter edit never
touches (see below).

### `platform` on a playthrough

A game's front matter has a `platform:`, but that can only describe one, and the same game does
get played on more than one — Persona 5 Royal on PS4 and again on Steam when it released there,
Octopath Traveler finished on PC and dropped on Switch. Those are separate runs with separate
dates and separate outcomes, so an entry may carry its own `platform:`:

```yaml
playthroughs:
  - started: "2020-05-19"
    finished: "2020-10-17"
    status: mastered
    platform: PS4
  - started: "2023-03-12"
    finished: "2023-05-14"
    status: mastered
    platform: PC
```

It's `omitempty`, and **blank means "inherit the game's `platform:`"** rather than "unknown". That
is what makes every file written before the field existed still mean what it did, and it's why the
form offers the game's platform as a *label* on the blank option instead of prefilling it —
prefilling would freeze a copy, so a later correction to the game would silently fail to reach its
runs. Clearing the field in `gamelog` therefore declares `playthroughs[N].platform` as an allowed
removal: handing a run back to the game is the intent, not loss.

The games list and timeline render it per row, but only when a game's runs actually differ.
Labelling one run `PS4` and leaving its sibling bare would make the reader infer what the bare one
was; repeating `PC` on every row of the ~200 single-platform games is noise. `layouts/games/term.html`
counts distinct platforms across the entries and shows the column only when there's more than one.

Note this is a *human* judgement, deliberately, and not the same thing as per-playthrough provider
attribution — which is still not implemented, because the API data genuinely can't support it (see
`gamelog suggest`). You know you played it on PS4; Steam's achievement history can't tell you.

Game directory names come from `Slugify`, which folds accents to ASCII (`Ōkami` → `okami`,
`Pokémon Red` → `pokemon-red`) and drops symbols like `™`/`®`. A title that yields no slug at all
is refused rather than written, and a slug already in use names the game holding it — distinct
titles can collide (`Hades: II` and `Hades II` both give `hades-ii`).

## How front matter is edited

`_index.md` stays conceptually hand-authored — unlike `playthroughs.yaml`, the tool doesn't own
the whole file, so there's no re-emitted banner and no attempt to reformat the file each write.
An edit is a **splice**: the bytes before the front matter and everything from the closing `---`
onward (the markdown body) are kept exactly as read, and only the front-matter block between the
delimiters is replaced. A hand-written overview paragraph below the front matter is never at risk,
because it's never reconstructed — only ever copied verbatim from the original file.

Title, platform, status, dates, rating, and `draft` are editable, through a game's edit page in
`gamelog serve`. `retroachievements_id`/`steam_appid` are deliberately not — relinking a provider
ID is a more consequential action than a date fix, and isn't implemented yet.

`status: mastered` is `finished`'s stronger sibling — the game was beaten *and* every achievement
was earned, not just the ones that come from beating it. It's a distinct value rather than a flag
on `finished` so the timeline/list can color and filter them separately.

`status: paused` is distinct from `dropped`: it means the game is on hold, not abandoned — you
mean to come back to it eventually. Setting it never writes a finished date, since the game isn't
finished; `gamelog stale` in particular offers it as a one-click correction on a quiet game it
otherwise would have guessed `dropped`, for exactly that "no, I'll get back to it" case.

`status: misc_launch` (entry-level only, not a `gameStatuses` value) covers a playthrough entry
that isn't a real attempt at all — booted up just to check dates, or a launch that never got past a
broken platform port (e.g. a Linux build that wouldn't run) — as distinct from `dropped`, which
implies play was actually attempted and abandoned. Unlike the one-shot statuses below, a second
`misc_launch` entry on the same platform is *not* fragmentation — each launch is its own unrelated
occasion — so it's excluded from `isOneShot` and can repeat freely.

`status: unplayed` (game-level only) is for a game that was launched or touched somehow but never
actually played — booting a Switch title solely for a cross-save/gift unlock is the case that
prompted it — and, unlike `backlog`, makes no claim about ever getting to it. `backlog` says
"haven't gotten to it yet"; `unplayed` says "no plan either way."

`ongoing`, `multiplayer`, and `software` are the three **one-shot** statuses — an entry with
no meaningful start/finish narrative, used in an open-ended series of sessions with no state that
ends play. They're separate statuses because the *reason* differs: `ongoing` is a replay-loop
design (roguelike/sandbox/idle), `multiplayer` is inherently social, and `software` isn't a game at
all — Steam sells tools alongside games and reports playtime for them identically, so they arrive
through the same `gamelog scan` and need somewhere to go that isn't a completion state.

Any of the three gets exactly one `playthroughs.yaml` entry **per platform**, whose `sessions:`
list *is* the record — so there's nothing to accidentally fragment into a second discrete
playthrough on the same platform later. The cap is per-platform rather than absolute because
saves don't cross consoles: Spelunky 2 on PS4 and on Steam is two separate records with two
separate achievement sets, which is not the fragmentation the cap exists to prevent.

`forms.IsOneShot` is the predicate for the status; `interactive`'s `doNewPlaythrough` enforces the
per-platform half, which can only be checked once the form has collected the platform — so a
same-platform second entry is refused after the form rather than hidden from the menu.
`model.EffectivePlatform` resolves blank to the game's platform, so an entry predating the field still
compares equal to its own game.

Note that the entry's own status stays `playing` — these three exist only in the game-level
vocabulary, so anything reading entry status (the timeline, notably) has to special-case them.

`status: backlog` is the game-level marker for "not played yet" — a game with this status and no
`playthroughs.yaml` at all. It's excluded from the games list's year-grouped table and shown
instead in the Games page's own "Backlog" section (`layouts/partials/games-backlog.html`).

## Planned replays

A `status: planned` entry in `playthroughs.yaml` is a dateless placeholder — "I intend to (re)play
this," with no `started` yet:

```yaml
playthroughs:
  - started: "2021-05-01"
    finished: "2021-05-15"
    status: finished
  - status: planned
    platform: PC
    notes: Try NG+ mode this time
```

"Add a planned replay" (always offered on a game's page in `gamelog serve`) creates one; it
refuses a second placeholder for the same effective platform, the same "don't fragment into two
bookmarks for one intent" reasoning as the one-shot statuses' per-platform cap, though `planned`
isn't itself part of
that family (`forms.IsOneShot` returns `false` for it, and it's deliberately absent from
`forms.PlaythroughStatuses` — see below).

Because a blank `started` is what every date-based partial (`games-timeline.html`,
`game-year-rows.html`) already guards on before turning an entry into a row or a timeline bar, a
planned entry is automatically invisible to both — no template change was needed to keep it out of
the timeline. `games-backlog.html` surfaces it explicitly instead, alongside `status: backlog`
games. `layouts/games/term.html`'s generic per-entry rendering also needed no change: it already
renders any entry's `status` as a plain pill and leaves a blank meta row when dates are empty.

"Manage a planned playthrough" (offered once at least one planned entry exists) either graduates it
— `GraduatePlannedPlaythrough` fills in `Started`/`Finished`/`Status`/etc. **in place**, at the same
array index, rather than replacing the entry — or edits its platform/notes without touching status
or dates. Graduating in place matters for the loss-check: every field it sets was previously empty,
so `model.CheckNoFieldLoss` sees a pure addition and no `allowedRemovals` declaration is needed.

`planned` is deliberately **not** in `forms.PlaythroughStatuses` — that list feeds the
status `Select` in `PlaythroughForm` (new playthrough, which requires a `Started` date),
`UpdateForm`, and `SplitStatusForm`, none of which should ever produce or accept a dateless entry.
The only two code paths that can set or clear `planned` are `interactive`'s `doAddPlannedReplay` and
`GraduatePlannedPlaythrough`.

There is deliberately no "delete a planned entry" action. Removing an entry from `playthroughs[]`
by index would shift every later entry down, and `SplitPlaythrough` (above) always appends its new
entry at the true end of the slice specifically so no existing entry's index — and therefore no
path into a later entry's own nested `sessions:` — ever moves. A stale planned placeholder can be
deleted by hand; `playthroughs.yaml`'s own generated banner already says the file is safe to edit
that way.

The same two safeguards as `playthroughs.yaml` apply: an `Extra map[string]any` inline field
catches `cover`, `cascade`, and anything else the struct doesn't model, so it round-trips instead
of vanishing on a rewrite; and the pending rewrite is diffed against the bytes on disk before
being allowed to land. One difference from `playthroughs.yaml`: front-matter scalar fields
(`started`, `rating`, ...) are **not** `omitempty`, so clearing one leaves the key present with a
blank/null value rather than dropping it — matching how a freshly-created file already looks
(`started:`, `rating:` with nothing after the colon). That means clearing a field is never
reported as data loss; there's nothing to declare.

Re-encoding the front-matter block through `yaml.v3` won't reproduce today's hand-formatted
quoting exactly — `title: "X"` may come back as `title: X` or `title: 'X'`, and a blank field may
render as `null` instead of bare. This is cosmetic, not lossy: the loss-check compares decoded
field *presence*, not text, and the same tradeoff is already accepted for `playthroughs.yaml`.

## `gamelog suggest` — playtime date suggestions

`gamelog suggest [slug]` queries RetroAchievements and Steam for a game and prints suggested
`started`/`finished` dates to the terminal. **It never writes to any file.** Review the
suggestion, then enter the dates yourself through `gamelog serve`.

```
go run ./cmd/gamelog suggest okami        # direct, by slug
go run ./cmd/gamelog suggest              # interactive game picker
```

RetroAchievements achievement-unlock dates are close to ground truth (RA is achievement-first).
A game with a finishing award — `mastered`, `completed`, `beaten-hardcore`, or
`beaten-softcore` — reports **high** confidence and uses the award date as the suggested
finish; anything else falls back to the latest unlock at **medium** confidence.

Steam has no playtime-history API at all — its suggestion is a guess derived from achievement
*unlock* timestamps, and is only available for games that have achievements. It is always
**low** confidence.

Either provider is silently skipped (with a one-line reason) if a game has no matching ID or
credentials aren't configured — a missing provider never aborts the command. The two cases
Steam reports as HTTP errors, "requested app has no stats" and "profile is not public", are
surfaced as plain skip reasons rather than raw status codes.

All dates are reported in `America/Chicago`, matching `timezone` in
`config/_default/hugo.toml`, so a late-evening session isn't attributed to the next day.

Because RA/Steam only expose one achievement history per game, a suggestion reflects a game's
*entire* history, not any single logged playthrough. If a game has more than one playthrough,
the report says so — cross-reference manually before entering a date.

## `gamelog serve` — the web UI, and its Housekeeping page

`gamelog serve` (also what bare `gamelog` runs) serves a local web UI over `content/games`:
create/edit/delete games, log playthroughs and sessions, manage planned replays, and reorder or
split sessions. Its **Housekeeping** page is where the bulk maintenance flows live — scan,
backlog, unfinished, stale, close, and achievement refresh-all — described below by what each one
does; on the page itself these are filter fields, checkboxes, and buttons rather than CLI flags.

**Scan** lists games on RetroAchievements and Steam that have no entry in `content/games` (a
`min_hours` field narrows the Steam side, default 5), one row each with the evidence behind it and
the entry it would create. Matching is by external ID where one is set, falling back to a
normalised title — so a game logged before those fields existed is still recognised, and casing or
a trailing `®` doesn't produce a duplicate (`elden-ring` matches `ELDEN RING`). Anything created
is written with `draft: true`, because every field is inferred — review it and flip the flag to
publish. Scanning is essentially the two bulk endpoints (one request each), so it's fast and can't
be rate-limited — the one exception is a **RetroAchievements subset row**, which costs one
per-game request to resolve its parent, memoized for the process (see "RetroAchievements subsets"
below). The bulk endpoints carry no start dates; create the entry, then run `gamelog
suggest <slug>` for the precise range. A RetroAchievements game counts as finished when it carries
a real award — the row names which one, since `12/189 achievements → finished` only makes sense
once you can see it was `beaten-hardcore` rather than a mastery. A `mastered`/`completed` award —
every achievement, not just the ones needed to beat it — is suggested as `status: mastered`
instead of `finished`. **Steam games are never auto-marked finished**: playtime alone says nothing
about completion.

**Every guessed row follows the same three-part shape as the Stale section**, which is the
paradigm to keep reaching for here: the guess as the primary button (*Correct — create finished*),
the other plausible answers beside it as *Create as* + a dropdown (which includes `software`, for
the tools Steam reports playtime for identically), and a way out that isn't "create it and fix it
later" (*Never log this*). The alternatives are a dropdown rather than a button each — unlike
Stale, which shows a handful of cards, this is a row per unlogged game, and five buttons apiece
was most of the page. **The dropdown only counts when its own button was clicked**: a `<select>`
submits its value whichever button sent the form, so the primary button would otherwise create
whatever the dropdown was left showing — hence the `use_status` flag its button carries.

Overriding to a status that doesn't assert the game is over drops the inferred finish date with it
— the date only existed to justify the guess, and keeping it under `status: playing` would write a
self-contradicting entry. A submitted status is validated against `forms.GameStatuses` before it
reaches front matter.

**A RetroAchievements subset gets a different primary button** — *Attach to \<base game\>* rather
than *create* — because creating it would make a second entry for a game that already has one.
See "RetroAchievements subsets" below for what attaching writes and how the parent link is
verified; the row is otherwise identical, alternatives dropdown and *Never log this* included.

Scan-created games land as `draft: true`; the games list index has a drafts-only filter with a
**Draft?** column, and each one's page has Publish/Undraft, Edit, and Delete — the latter only
offered when the game has no logged playthroughs *and* no archive record for either linked
provider ID, since once either exists it isn't a plausible false-positive scan match anymore and
deleting it would risk real data.

**Backlog** is the same Steam scan against the other side of `min_hours`: games you own but have
played for less than that and never logged. Scan and Backlog partition the library at one shared
line — the threshold itself counts as played — so a game is always in exactly one of the two
lists, and moving the field moves games between them rather than duplicating them. Creating one
writes `status: backlog` (see above: the game-level marker for "not played yet") with no
`finished` date and, by definition, no `playthroughs.yaml`; it's still a draft like everything
scan creates. RetroAchievements is skipped here entirely — it has no concept of ownership, and
`GetUserCompletionProgress` only returns games with at least one achievement already earned, so
nothing it reports could be backlog. Each creation also captures the game's achievement list (0
of N — the denominator is the point), which is one Steam request per game, so work through these
in batches rather than the whole library at once. Its alternatives are a deliberately smaller set
than Scan's — everything in this list is under the playtime threshold, so any status claiming real
history would be contradicted by the evidence that put it here; `unplayed` against `backlog`
("don't know if I ever will" versus "haven't gotten to it") is the distinction worth one click.

**Ignored** is the escape hatch for games you're never going to log — a demo, a bundle leftover, a
tool that registers as a game. It's the escape hatch on every Scan and Backlog row — **Never log
this** — and writes an entry to `archive/ignored.yaml` that both lists skip from then on. The list is keyed by provider ID only (never by title, unlike the already-logged check — a
title match there recovers pre-ID entries, but here it could silently hide a *different* game that
happens to share a name), so the same game can be ignored on Steam and still offered on
RetroAchievements. Nothing is deleted or hidden anywhere else: an ignored game keeps whatever
archive records it has, and the Ignored section lists every entry with an **Un-ignore** button
that puts it straight back in the next scan. The Ignore buttons post the provider and ID rather
than a row index, so an ignore doesn't depend on the re-run scan coming back identical the way the
Create buttons do.

**Unfinished** ranks logged games by how close they are to having every achievement, closest
first. It reads only what's already in `archive/<provider>/<id>.json` — no API calls, nothing
written — so it renders with no credentials configured at all, and it's a reading list rather
than an action queue: each row links to the game's own page. A `min_pct` field (default 50) hides
games that were opened once rather than nearly finished; those belong in Backlog above. One row
per provider, not per game: a game linked to both Steam and RetroAchievements has two unrelated
denominators, and merging them would invent a number that's true of neither. A RetroAchievements
subset is another such denominator, so it gets its own row too, named (`retroachievements · Mouse
Alley`) rather than repeating the bare provider on two rows with different numbers. The one-shot
statuses (`ongoing`, `multiplayer`, `software`) never appear — there's no completion for them to
be short of — and `finished`/`dropped`/`mastered` games are opt-in via a checkbox, since leaving
achievements on a game you've called finished is a decision, not an oversight. `paused` and
`backlog` games *are* listed: both mean "not now", not "not ever".

**Stale** uses Steam's `GetOwnedGames rtime_last_played` (captured into every Steam archive record
as `last_played`) to find games you've probably stopped playing but never marked as such —
currently `status: playing`, Steam-linked, and untouched for a while (`stale_days` field, default
30) — draft or published; draft ones are tagged `[draft]` so accepting a suggestion is clearly not
the same as publishing. For each one it computes a guess — **mastered** at 100% achievement
completion, **finished** at ≥90%, **dropped** otherwise, or **dropped at low confidence** when the
game has no achievements to go on at all (playtime alone doesn't prove completion, so "dropped" is
the safer default, not a claim) — alongside the last-played date, achievement count, and playtime.
Nothing is ever written automatically: accept the guess, pick one of the other statuses it might
have guessed instead, open the full edit form prefilled with the guess, or leave it and it'll
surface again next time it's still `playing` and still quiet. None of the one-shot statuses
(`ongoing`, `multiplayer`, `software`) ever shows up here — they aren't `status: playing` by
definition (see above), and something used in indefinite session bursts has no "done" to detect
from staleness alone.

**Close** finds every playthrough whose trailing date is blank and offers to cap it at the last
day it was actually played — **never changing a status**, only writing a date. This is a different
question from what Stale asks: Stale looks at games still marked `playing` and guesses a
*terminal status* from achievement completion; the entries Close targets are mostly one-shot games
whose status is already correct, where the only thing wrong is a trailing `finished: ""` that
renders as "ongoing" forever. A sandbox last touched in 2017 isn't mid-playthrough, but it isn't
"finished" either — closing the date and picking a status are separate decisions. The whole list
is presented pre-selected (`close_days` field, default 30; `close_all` checkbox to include
`playing` games too — `paused` games are excluded either way, since that status means on hold, not
abandoned, and an open-ended bar is the honest render for something meant to be resumed), so the
cheap interaction is deselecting the exceptions rather than confirming each one. The closing date
comes from the archive's `last_played` when there is one, falling back to the open session's own
`started` date — the fallback is the right answer more often than it looks, since a party game
played on one evening has a start date and nothing else, and that evening *is* when it stopped.
Each row shows which source it used, so a wrong date is visible before confirming. Anything with
no date anywhere is left alone; inventing one would be worse than leaving it open. Writes go
through the same pre-write proof as every other playthrough write: the pending rewrite is diffed
against what's on disk and refused if a field would vanish, and an entry closed between building
the list and confirming it is left alone rather than overwritten.

**Refresh all** runs the achievements fetch-and-merge (below) across every game with a provider
link, in the background — the page redirects to a job status view while it runs.

## `gamelog achievements` — full unlock history

`gamelog achievements [slug]` writes every achievement, with its unlock timestamp, to
`archive/<provider>/<id>.json` — one file per provider, resolved from the game's
`retroachievements_id`/`steam_appid`. `scan` does this automatically for each entry it creates;
run the command directly to refresh a game or backfill one that predates it.

```
go run ./cmd/gamelog achievements spelunky
go run ./cmd/gamelog achievements --all    # refresh every game with a provider link
```

`--all` is the same fetch-and-merge call as the single-slug path, just looped over every game
`ListGames` returns that has a provider link — it inherits the merge-only guarantees below rather
than needing its own, which is what makes it safe to point an unattended, scheduled run at.

### Closing the gap with the timeline

Everything above only touches `archive/` and `achievement-summary.yaml` — never
`playthroughs.yaml`. That matters because the timeline's visible bars are built from
`playthroughs.yaml` alone (see `layouts/partials/playthrough-entries.html`); `achievement-summary.yaml`
only feeds a game's `last_played` sort key. Refreshing a game's achievements can therefore leave the
archive fully up to date while the timeline shows nothing new — the fix isn't a rebuild, it's that
nobody logged a session for the activity that just got captured.

For the one-shot statuses (`ongoing`/`multiplayer`/`software` — see above), where the whole
playthrough record *is* its `sessions:` list, the single-slug `gamelog achievements <slug>` closes
that gap itself: if the refresh pulled in playtime or an unlock date past what's already logged, it
offers to log a session right then, prefilled with the activity date the provider reported (that's
all a playtime/last-unlock signal can tell you — a real multi-day range is still yours to correct).
Declining leaves nothing written. This only fires for a game with exactly one playthrough entry; a
rarer multi-platform one-shot game is left to "Log a new session" by hand, same as before.
`gamelog achievements --all` never prompts — it's meant for unattended runs, so a one-shot game
refreshed that way still needs a manual session log afterward.

The archive itself is deliberately not reachable from templates — see `gamelog project` below for
how a slice of it (achievement counts) reaches the site without that changing.

### The two rules that shape this file

**1. A refresh can never lose data.** Providers fail in ways that look like success — a profile
goes private, a game is delisted, a key is revoked, a rate limit returns an empty body. Any of
those would otherwise overwrite years of history with nothing. So writes **merge**: an unlock
recorded once is never retracted, its original date wins over a later one, and a fetch that
returns nothing usable updates only `last_attempt`/`last_error` while leaving the data alone.
`gamelog achievements` reports `(existing data kept)` when that happens, and still exits 0.

**2. Keep everything the API gave us.** Locked achievements are kept (they're the denominator,
and a game gaining achievements later is itself a fact worth having). So are badges, points,
hardcore flags, per-device playtime, and the provider's **verbatim response** under `raw` — so a
field nobody normalised is still recoverable without re-fetching, which may be impossible if
access is lost.

### Shape

One record per file, at `archive/<provider>/<id>.json`:

```
provider                  "retroachievements" | "steam"
title                     the provider's own name for the game, so an orphaned
                          record stays identifiable on its own
updated
id, fetched, last_attempt, last_error
unlocked / total          earned vs. existing — locked ones are in the list too
first / last              YYYY-MM-DD of earliest and latest unlock
award_kind / award_date   the completion award, when there is one
platform, icon, completion
playtime_mins, playtime{windows,mac,linux,deck,disconnected}, last_played
achievements[]            key, name, description, unlocked, date, points, hardcore, icon
raw                       the provider's verbatim response
```

Unlocked achievements sort first in date order, locked ones last. Dates are RFC3339 with the
site's UTC offset (DST-correct), which is what Hugo's `time` function expects — no conversion
needed in templates.

`last_played` is a *played* date, not an unlock date, and every provider that can report one does:
Steam's `rtime_last_played`, PSN's, and — since it needs a second endpoint —
RetroAchievements' `API_GetUserRecentlyPlayedGames`. RA's progress endpoint has no such field, so
before that list was walked an RA game's most recent date was whatever it last unlocked, which
sits still for as long as a playthrough is stuck on one achievement. That list is user-level, not
per game, so it's fetched **once per process** and cached (`raRecentlyPlayed` in
`internal/commands/achievements.go`) — at RA's ~1 request/1.2s throttle, re-walking its pages for
each of 200 games would cost more than the refresh itself. A failure to fetch it is warned about
and ignored: `last_played` falls back to the newest unlock, same as before.

This is why history is kept per-achievement rather than collapsed to a start/finish pair: a game
returned to over years (Spelunky's unlocks run 2013 → 2020 with multi-year gaps) has a shape two
dates can't express. It's also why both providers are kept — the two records genuinely differ,
and merging them is a presentation decision, not a storage one.

### Linking a game to external IDs

Add any of these to a game's front matter (blank/omitted is fine — that provider is just
skipped):

```yaml
retroachievements_id: 4650      # numeric ID from the game's retroachievements.org URL
steam_appid: 1145360            # numeric appid from the game's Steam store URL
psn_id: "NPWR16532_00"          # PlayStation trophy-set ID (see below)
ubisoft_id: "12345"             # Ubisoft Connect canonical ID, via Exophase (see below)
xbox_id: 1480657033             # Xbox titleId, via OpenXBL (see below)
```

The interactive "new game" form accepts either the bare number or the full URL you copied it
from (`retroachievements.org/game/4650`, `store.steampowered.com/app/1145360/Hades/`) and
stores the bare ID. `suggest` accepts a pasted URL in front matter too.

Everything that walks a game's archive takes `[]providerLink` (from `Doc.ProviderLinks` or
`GameSummary.ProviderLinks`) rather than a positional `(raID, steamAppID)` pair, so a fourth
provider is a client file plus one entry in `providerOrder` — not an edit to every signature
that touches the archive.

### RetroAchievements subsets

RetroAchievements publishes a bonus achievement set as a **separate game id**, titled
`<base game> [Subset - <name>]` — Professor Layton and the Last Specter's "Mouse Alley" is one.
Left alone every subset shows up in Scan as an unlogged game, and creating it makes a second
entry for a game that already has one. So a subset is *attached* to the game it belongs to
instead, as a second RA id on that game:

```yaml
retroachievements_id: 7601
retroachievements_subsets:
  - 25709                       # Professor Layton … [Subset - Mouse Alley]
```

That's the only place in the schema where one game has two ids on one provider.
`BuildProviderLinks` emits each subset as its own `ProviderLink` with `Subset: true`,
immediately behind the base RA link, so **capture, refresh and projection all pick subsets up
for free** — `gamelog achievements <slug>` fetches and merges each set into its own
`archive/retroachievements/<id>.json`, and `--all` inherits that without knowing subsets exist.
The archive needed no change at all: a subset was always just another game id to it.

**Only the counts are kept apart.** `achievement-summary.yaml` puts a subset on its own
`subsets:` line and leaves it out of the headline `unlocked`/`total` and the `providers:`
breakdown:

```yaml
unlocked: 16          # the game's own set — not 22
total: 40             # not 92
providers:
  - provider: retroachievements
    platform: Nintendo DS
    unlocked: 16
    total: 40
subsets:
  - name: Mouse Alley
    provider: retroachievements
    id: "25709"
    platform: Nintendo DS
    unlocked: 6
    total: 52
    playtime_mins: 202
```

A subset is a bonus set for the same game, not a second copy of it: adding Mouse Alley's 52 to
Last Specter's 40 would restate a 40%-complete game as 24% complete and describe a 92-achievement
set that exists nowhere. Playtime is kept on the subset's line for the same reason — RA reports it
per game id, and the same hours would otherwise be counted twice. `last_played` *is* folded in,
because playing the bonus set is playing the game.

The unlocks themselves stay in the one flat `earned:` list, each tagged `subset: Mouse Alley` —
they were genuinely earned, and the site-wide achievement feed would otherwise silently drop
them. `layouts/partials/game-achievements.html` renders a **Subsets** section under the game's
meter, and both achievement feeds label a tagged row.

**The link is never inferred from the title.** The bulk endpoint Scan runs on carries no parent
field at all, so the `[Subset - …]` convention is only used to *spot* a candidate; the per-game
endpoint's `ParentGameID` is what actually links one, and `commands.AttachSubset` re-checks it
against the target game's own `retroachievements_id` before writing anything — a submitted form
value must not be able to put another game's achievements on this game's page. That costs one
extra RA request per subset row, memoized per process (`raParentCache`), since Housekeeping
re-derives its scan on every render and RA throttles to ~1.2s a call.

On the Housekeeping page a subset row keeps the usual three parts but swaps the primary button:
**Attach to \<base game\>** when the base game is logged, **Create \<base game\> + attach** when it
isn't (which creates the base game from RA's own report of *it*, as a draft, like everything else
Scan creates), and an ordinary *Correct — create …* only when the parent couldn't be resolved at
all. *Create as* and *Never log this* stay put either way. Attaching a subset also takes it out
of future scans, and the game's page in `gamelog serve` lists what's attached to it.

Subsets are display-and-capture only: nothing about them touches `playthroughs.yaml`. They're
deliberately *not* wired into the `subgames:` roster either — a subset is a second achievement
set for one game, not a distinct campaign that could carry its own playthrough.

### PlayStation, via Sony's own trophy API

PSN trophy history is read from **Sony's own trophy API (v2)** — the same one the PS App itself
uses — documented unofficially at
[andshrew's PlayStation-Trophies docs](https://andshrew.github.io/PlayStation-Trophies/#/APIv2)
(reverse-engineered; Sony has never published one). This replaced an earlier version that mirrored
the data through a public Exophase profile; `Source` on `ProviderRecord` exists specifically so a
record fetched the old, mirrored way is distinguishable from one fetched first-party — see
`internal/model/archive.go`. `psn_id` is the `NPWR…` trophy-set ID (`npCommunicationId`), Sony's
own and permanent, and both the old and new clients key the archive on it, so no migration of the
front-matter field was needed.

**Auth is an npsso session cookie, exchanged for a short-lived bearer token — not a stable API
key.** Get one by logging into `store.playstation.com`, then in the same browser visiting
`https://ca.account.sony.com/api/v1/ssocookie`; the response is `{"npsso":"<value>"}`. That value
goes in `PSN_NPSSO` (a real secret, unlike `EXOPHASE_USER` below) and is good for ~2 months. Each
`PSNClient` exchanges it for an OAuth access token (valid ~60 minutes) once, on first use, and
reuses that token for the rest of the process — there's no refresh-token persistence across runs,
since a CLI invocation finishes well inside the token's lifetime.

Three calls make up a fetch, all under `/api/trophy/v1` on `m.np.playstation.com`:

1. `users/me/trophyTitles` — every title on the account, including each one's `npServiceName`
   (`"trophy"` for PS3/PS4/Vita, `"trophy2"` for PS5/PC — **required** on the other two calls for
   a `"trophy"` title). Fetched once per run and cached by `npCommunicationId`; this is also what
   backs `gamelog psn list`.
2. `npCommunicationIds/{id}/trophyGroups/all/trophies` — each trophy's static metadata: name,
   description, icon, tier (bronze/silver/gold/platinum).
3. `users/me/npCommunicationIds/{id}/trophyGroups/all/trophies` — the account's earned status per
   trophy: unlocked or not, and when.

The client joins 2 and 3 on `trophyId` client-side — no single Sony response carries both. A
platinum unlock sets `AwardKind`/`AwardDate` on the record, matching how Steam reports "all
achievements". Trophy tier (bronze/silver/gold/platinum) is stored in each achievement's `Tier`
field — a real API field, not a derived guess, and one no other provider has an equivalent for.

There's no lookup UI for `npCommunicationId` in Sony's own apps, so run `gamelog psn list`: it
prints every trophy title on the account next to its ID and current unlock count, sorted by title,
ready to paste into `psn_id`.

Records are merged by exactly the same never-lose rules as every other provider — a bad fetch (an
expired `npsso`, a title never launched on this account) updates `last_attempt`/`last_error` and
leaves history alone. Sony's documented rate limit is ~300 requests per 15 minutes (unconfirmed by
Sony itself); `PSNClient` paces requests the same way `RAClient` does.

### Ubisoft, via Exophase

**Ubisoft has no public achievements API at all**, so Ubisoft Connect history is read from a
public [Exophase](https://www.exophase.com) profile instead, which mirrors it including
per-unlock timestamps — the part actually worth archiving. This is the same mechanism PSN used to
use; see git history / the section above if you're wondering why PSN isn't here anymore.

`ubisoft_id` is the canonical ID Exophase reports for the `uplay` environment, from Exophase's own
`canonical_id` field — the archive keys on that rather than on anything Exophase owns, so records
survive the mirror going away.

The ID isn't shown anywhere in Exophase's own UI — `canonical_id` only exists in a JS payload
embedded in the profile page's HTML (see `canonicalIDs` above). Rather than view-source-diving for
it, run `gamelog exophase list ubisoft`: it prints every game on the configured `EXOPHASE_USER`
profile next to its canonical ID and current unlock count, sorted by title, ready to paste into
`ubisoft_id`.

There are no secrets involved: `EXOPHASE_USER` (`EXOPHASE_PSN_USER` still works as an alias, from
when this also covered PSN) is a public profile name, and the profile just has to be public, with
Ubisoft Connect linked to it. Records are written with `source: exophase` and are merged by
exactly the same never-lose rules as every other provider — a private profile or a failed fetch
updates `last_attempt`/`last_error` and leaves the history alone.

Three quirks, all found against live responses:

- Cloudflare serves an interstitial to Go's default `Go-http-client/1.1` User-Agent. Requests
  must send a browser one (`exophaseUserAgent`) — the same class of trap as RA's 403.
- **The JSON API's per-game `meta` omits `canonical_id`.** The real ID only appears in the
  `window.playerGames` payload embedded in the profile HTML, so the client reads both and joins
  them on Exophase's own `master_id`.
- Only the **account** page (`/user/<name>/`) carries the widgets naming each linked service's
  player id; the per-service pages don't, and their usernames differ from the account name
  anyway (Nintendo's is an opaque hash). So the account page is read first and its own
  `data-endpoint` links are followed.

A game linked to more than one provider keeps **both** figures in `achievement-summary.yaml`:
the combined total, and a `providers:` breakdown of the numbers before they were added. The
games list renders the breakdown, because achievement sets don't combine across platforms —
Persona 5 Royal is 53/53 on PS4 and 53/53 on Steam, and "106/106" describes a set that doesn't
exist:

```yaml
unlocked: 106          # still recorded: "how many in all" is a real question,
total: 106             # it just isn't the one the games list asks
providers:
  - provider: steam
    platform: PC
    unlocked: 53
    total: 53
  - provider: psn
    platform: PS4
    unlocked: 53
    total: 53
```

The breakdown is labelled by `platform`, not by provider — `PS4` is what a reader recognises;
`psn` is plumbing. A game on one provider renders the combined figure as before, which is the
same number either way.

### Xbox 360, via OpenXBL

**Microsoft has no public achievements API either.** Unlike Ubisoft, there's no Exophase-style
public mirror for Xbox, so this goes through [OpenXBL](https://xbl.io) instead — an unofficial API
that requires signing in with the actual Microsoft account and creating a personal key on your
xbl.io profile page (`XBLIO_API_KEY`). That key is a real secret, not a public profile name like
`EXOPHASE_USER` — OpenXBL only ever reads the key owner's own account.

Two quirks, found against live responses:

- **The modern per-title achievement endpoint returns an empty list for legacy Xbox 360 titles.**
  A separate endpoint (`/achievements/x360/{xuid}/title/{titleId}`) exists specifically for them,
  and `xbox.FetchRecord` always uses it — there's no non-360 Xbox title in this archive yet.
- **That endpoint only reports achievements that are actually unlocked**, the same shape as
  Exophase's earned list. The total-possible count and last-played date come from a separate
  `player/titleHistory` call instead, which lists every title on the account in one response.

A handful of `archive/xbox/*.json` records predate the live client: they were seeded from a
one-time pasted OpenXBL export, before `XBLIO_API_KEY` existed. Those are summary-only —
`unlocked`/`total` set directly, no per-achievement list — and `mergeProvider`'s `Summarize` step
would silently zero `unlocked` for one if it ever ran through the normal merge path, so they were
written straight to `archive/`, not through `SaveRecord`. Refreshing one of those games now (with
a key configured) fetches real per-achievement data and merges normally from then on, the same as
any other provider.

### Trackmania, via Nadeo's own web services

**This is the one provider that doesn't record achievements.** Trackmania's real history is a
personal best time on every track of every official campaign — ~450 tracks across 18+ seasons —
and there is no way to express that as unlocked/total. So it has its own record type
(`model.NadeoRecord`), its own merge, its own projection (`campaigns.yaml`), and it is deliberately
**not** part of `ProviderLinks`/`ProviderOrder`. Folding it in would restate Trackmania's headline
meter as 433/480 of an achievement set that doesn't exist.

`gamelog nadeo whoami` prints the account id for `nadeo_account_id`; `gamelog nadeo seasons` lists
every campaign next to what's archived; `gamelog nadeo fetch` captures.

The API is documented (unofficially, by the community) at <https://webservices.openplanet.dev>.
Things worth knowing before touching `providers/nadeo`:

- **Auth is the service-account flow** — a login/password created at trackmania.com, exchanged at
  `/v2/authentication/token/basic` via HTTP basic auth. The older Ubisoft-ticket flow stopped
  working in April 2026; don't implement it. The login is the *service account's*, which is a
  different string from the Ubisoft username.
- **A service account inherits the Ubisoft account it was created on**, and it must be created on
  the account whose times you want. Both record endpoints return the *authenticated account's*
  data; a throwaway account returns an empty archive.

  Openplanet's docs generally recommend *against* binding to a primary account, since a ban would
  affect play — but they carve out an explicit exception for "when your use case requires one
  specific player's authentication information to access their own data", which is this. The
  alternative (`/v2/mapRecords/by-account/`, which reads any account with any token) accepts only
  **one map per request**, so avoiding the binding would mean ~450 requests per capture instead of
  ~2, and a separate account is no protection against an IP ban anyway.

  Because the credentials are a real player's, `nadeoMinInterval` is 500ms and `fetch` defaults to
  the current season only. A full backfill across all 25 official campaigns is ~15 requests and is
  meant to be rare.
- **There are two audiences and their tokens are not interchangeable.** The Live API (campaigns,
  leaderboards) needs `NadeoLiveServices`; the Core API (map metadata, medal times) needs
  `NadeoServices`. Both are fetched and cached separately, in memory only — never persisted, the
  same standing decision as PSN.
- **The account id comes out of the JWT payload**, not a separate call.
- **`mapUid` and `mapId` are different identifiers and are not interchangeable.** The UID is
  generated when a map is saved in the editor, the ID when it's uploaded. Leaderboard calls take
  UIDs; the Core record endpoints take IDs. Both are captured, and the map-metadata call is what
  translates between them — which is why it runs before records on either path.
- **Times are captured at two scopes, and neither can be derived from the other.** A record is
  filed under one leaderboard or another:
  - `scopeType: "Season"` — the best time set *while that campaign was open*. Frozen when the
    season closes. Fetched with `seasonIdList`, one request for every season at once. This is
    what matches the hand-written notes in `playthroughs.yaml`.
  - `scopeType: "PersonalBest"` — the all-time best on the map, which keeps moving when an old
    track is revisited to clean up a medal. Fetched with `mapIdList`, ~150 maps per request. This
    is what the game itself shows, and what the campaign grids display.

  They must be two passes: `mapIdList` and `seasonIdList` both filter every result, so sending
  them together intersects rather than unions. **The response must also be filtered on
  `scopeType`** — a `mapIdList` request carries no season filter, so season-scoped entries come
  back alongside the all-time ones and would otherwise be mistaken for them.

  The gap between the two is the record of having gone back to a track;
  `NadeoTrack.ImprovedSinceSeason` reports when the *medal* (not merely the time) got better, and
  the grid marks those cells.

- **The Live `leaderboard/group/map` is the fallback for the season pass only.** It needs a
  request per 50 maps and reports no dates, but does return zone rankings, which the Core endpoint
  omits. There is no fallback for the all-time pass — the Live leaderboard is season-scoped by
  construction — so if that pass fails the season figure stands in for display and the archive
  keeps whatever all-time value it already had.
  - The Core endpoint works **only for the authenticated account** — others get a 403 — which is
    also why it can't be used from a throwaway service account.
  - `mapIdList` and `seasonIdList` must not both be sent: both filter every result, so together
    they intersect rather than union.
  - It returns at most the **1,000 most recent records**, and that's a ceiling, not a page. Fine
    against ~450 campaign tracks; worth remembering if TOTDs are ever added.
  - `gameMode` is documented as recommended but the client omits it — no filter returns
    everything, which is harmless since results are indexed by `mapId`. Campaign Race maps come
    back as `"TimeAttack"`, confirmed live, if it's ever needed.
  - `medal` is Nadeo's own grade: 0 none, 1 bronze, 2 silver, 3 gold, 4 author. On the first live
    capture it agreed with the medal derived from the map's thresholds on all 25 tracks, which is
    what licenses `NadeoTrack.Medal()` computing it rather than trusting the integer.
- **A `recordScore.time` of `4294967295` is a sentinel, not a time.** It's `math.MaxUint32`, used
  when a map author has set a secret threshold score. `AccountRecord.Usable` filters it, along
  with `removed` entries.
- **The leaderboard endpoint hard-caps at 50 maps per request and silently truncates past that** —
  exceeding it loses data rather than erroring.
- **The map-info endpoint has no item cap but 414s at a request URI of 8220 characters** (~300
  UIDs). Batches are 150.
- **Responses are not 1:1 with requests.** An invalid or unknown `(mapUid, groupUid)` pair is
  omitted from the response rather than returned empty, so results are matched by id — never
  zipped with the request by position.
- **The leaderboard endpoint intermittently returns `[]` for valid parameters.** `recordBatch`
  retries once before concluding a batch has no times.
- Medal thresholds (`authorScore`/`goldScore`/`silverScore`/`bronzeScore`) come from the *map*,
  not the player, which is what lets `NadeoTrack.Medal()` score a time without a second fetch.
  They're captured for undriven tracks too.
- `seasonUid` and `groupUid` are the same identifier for official campaigns. They are not for
  club campaigns, which this doesn't capture.

`--dump-raw <dir>` writes every verbatim response body to disk. That's how `testdata/` fixtures
are produced: run one scoped season, not a backfill.

### Credentials

Copy `.env.example` to `.env` (gitignored) and fill it in. `suggest` finds that file whether
it's run from this directory or the repo root. Real environment variables always take
precedence, so `RA_API_KEY=... go run ./cmd/gamelog suggest x` still overrides the file.

| Variable | What it is |
|---|---|
| `RA_USERNAME` | Your RetroAchievements username |
| `RA_API_KEY` | RetroAchievements API key, from your account settings on retroachievements.org |
| `STEAM_API_KEY` | Steam Web API key, from steamcommunity.com/dev/apikey |
| `STEAM_ID` | Your SteamID64 **or** the vanity name from your profile URL — a vanity name is resolved automatically. `STEAM_USER_ID` is accepted as an alias. |
| `EXOPHASE_USER` | Your Exophase profile name, for Ubisoft Connect achievements. **Not a secret** — it's a public profile, which just has to be public. `EXOPHASE_PSN_USER` is accepted as an alias, from when this also covered PSN. |
| `PSN_NPSSO` | npsso session value, for PSN trophies via Sony's own trophy API. **This is a real secret**, and it expires after ~2 months — see "PlayStation, via Sony's own trophy API" above for how to get one. |
| `XBLIO_API_KEY` | Personal API key from xbl.io/console, for Xbox 360 achievements. **This one is a real secret** — it's scoped to your own Microsoft account, unlike `EXOPHASE_USER` above. |
| `NADEO_SERVICE_LOGIN`, `NADEO_SERVICE_PASSWORD` | Trackmania service account, from trackmania.com's service account page, for campaign times. **Both are real secrets**, and the password is shown exactly once at creation. Must be created on the Ubisoft account you actually play on — see "Trackmania, via Nadeo's own web services" above for why. |

Steam's achievement/playtime endpoints only return data for a public profile, or your own
profile when the key you're using belongs to that account. Note that per-game achievement
privacy is separate from profile privacy, so an otherwise-public account can still return
"profile is not public" for one game.

### Notes for future maintenance

Every one of these APIs has sharp edges that aren't obvious from their docs, and each one silently
produced wrong or missing output before it was found by running against live data:

- RetroAchievements returns **403 for Go's default `Go-http-client/1.1` User-Agent.** Requests
  must send a descriptive one (`raUserAgent`).
- `API_GetGameInfoAndUserProgress` omits `HighestAwardKind`/`HighestAwardDate` unless **`a=1`**
  is passed — without it every suggestion silently degrades to medium confidence.
- That award date is **RFC3339**, while the per-achievement `DateEarned` fields are zoneless
  SQL datetimes. They need separate parsers (`retroachievements.ParseRAAwardDate` vs `retroachievements.ParseRADate`).
- An unknown RA game ID returns **HTTP 200 with a bare `[]`**, not a 404.
- **`ParentGameID` — the only real link from a subset to its base game — exists on
  `API_GetGameInfoAndUserProgress` and on no bulk endpoint.** `API_GetUserCompletionProgress`
  reports a subset exactly like any other game, distinguishable only by its
  `[Subset - …]` title. A base game answers with an explicit `null` there, which decodes as 0.
- `API_GetUserRecentlyPlayedGames` **caps `c` at 50** whatever you ask for, so a library with more
  than 50 played games needs `o` paging — without it the last-played lookup silently truncates to
  the 50 most recent. Despite the name it returns the user's whole client-tracked history, so a
  game absent from it was never seen by an RA client at all.
- Steam returns **400/401/403 with a useful JSON body** for its expected "no data" cases, so
  the body matters more than the status.
- Steam's API only accepts a 17-digit SteamID64; a vanity name must go through
  `ISteamUser/ResolveVanityURL` first.
- Exophase 403s Go's default User-Agent (Cloudflare), omits `canonical_id` from its JSON API,
  and only exposes per-service player ids on the account page. See "Ubisoft, via Exophase".
- PSN's OAuth authorize step hands back its authorization code as a query parameter on a **302
  redirect to a non-resolvable app-scheme URI** (`com.scee.psxandroid.scecompcall://redirect`) —
  the client must not follow it, only read `code` off the `Location` header. And a title whose
  service is `"trophy"` (PS3/PS4/Vita) **requires** `npServiceName=trophy` on every per-title call
  or the API answers as if the title doesn't exist; `"trophy2"` (PS5/PC) titles work with or
  without it. See "PlayStation, via Sony's own trophy API".

The committed `testdata/*.json` fixtures were corrected against live responses — keep them that
way, since a fixture written from the documentation is what let all of the above pass tests
while being broken in practice.

## `gamelog project` — achievement counts for the site

The archive lives outside `content/` on purpose (see above), so Hugo can never read it directly.
`gamelog project` is the projection step: it walks every game with a `retroachievements_id` or
`steam_appid`, sums that game's `unlocked`/`total` across whichever providers it's linked to (RA
12/40 + Steam 46/54 → 58/94) while also recording each provider separately under `providers:` plus
total playtime summed across whichever providers report it (Steam always has; RA added
`UserTotalPlaytime` in late 2025, so older archived RA records may predate it and simply have
none), and writes the result to `content/games/<slug>/achievement-summary.yaml` —
no `raw`, no per-achievement list, just the numbers the games list actually displays.

```
go run ./cmd/gamelog project
```

It only ever reads what's already in `archive/` — **no API calls**, so it's safe and fast to
re-run any time the projection needs rebuilding (after `gamelog achievements` refreshes a game, or
after the summary's shape changes) without touching credentials or rate limits. `gamelog scan` and
`gamelog achievements` already call the same write for whichever game they just touched, so a
full `project` run is only needed for a bulk backfill or to pick up an archive edited by hand.

A game with nothing archived for either provider gets no file — not a `0/0` one — and any stale
file left over from a cleared or relinked provider ID is removed rather than shown. A Steam game
with playtime but no achievements at all still gets one, just without the achievement line. The games list
(`layouts/games/taxonomy.html`) reads the file via `.Resources.Get`, which is silently absent for
those games, so the achievement count just doesn't render rather than showing a wrong or empty
count. Like `playthroughs.yaml`, the file is covered by `content/games/_index.md`'s
`_build.publishResources: false` cascade — Hugo can read it at build time, but it's never copied
to the public site.

### Not implemented (by design)

No relinking `retroachievements_id`/`steam_appid` or editing the markdown body from within the
tool, no `--json` output, no per-playthrough attribution (see above — the API data doesn't
support it), no CLI-flag credential overrides, and no rendering of the archive (see
`KNOWN-ISSUES.md`).
