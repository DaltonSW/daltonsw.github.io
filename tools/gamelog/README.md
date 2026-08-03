# gamelog

A terminal tool for maintaining this site's `content/games/<slug>/_index.md` playthrough log.

Run with no arguments for the interactive flow: pick or create a game, edit its info, log a new
playthrough, log a new session, or update an existing playthrough (finish date, status,
rating, notes). It loops — "Do another?" after each action — so a sitting can cover several
games without relaunching.

```
go run ./cmd/gamelog          # interactive
go run ./cmd/gamelog review   # walk through draft games one at a time
go run ./cmd/gamelog help     # usage summary
```

## Code layout

Standard `cmd/` + `internal/` Go layout. `cmd/gamelog/main.go` is just flag dispatch; everything
else lives under `internal/`, one package per concern:

```
internal/model         domain types + storage: front matter, playthroughs.yaml, the archive
internal/forms         huh-based prompts and the shared validation/status vocabulary
internal/mutate        confirm-preview-and-write-with-loss-check, shared by the TUI, the web
                        server, and the achievements command's own-session nudge
internal/providers/*   one package per API client (steam, retroachievements, exophase)
internal/dotenv        the tool's own minimal .env reader
internal/externalid    parses a pasted RA/Steam ID or URL into a bare ID
internal/commands      scan/suggest/stale/close/review/achievements — genuinely
                        interdependent (see the package doc comments), kept together
internal/server        the local web UI (`gamelog serve`)
internal/interactive   the terminal REPL (`gamelog` with no args)
```

`interactive` and `server` are two frontends over the same `model`/`forms`/`mutate` layer;
`commands` is the six flag-driven subcommands, which call into each other enough that splitting
them further would mean breaking a real (not accidental) dependency cycle between them.

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
```

**The archive is keyed by provider ID, not by slug, and lives outside `content/`.** A slug is a
presentation decision — it's the URL. When the archive was stored under one, renaming a game
moved it and deleting a game destroyed unlock history that may be impossible to re-fetch. Provider
IDs never change. Being outside the content tree also means Hugo never parses `raw`, and never
publishes it: bundle resources are copied to the built site, so the old layout was serving every
captured API response publicly.

A game on several services gets one archive file each. Nothing in the archive links them —
`retroachievements_id`, `steam_appid` and `psn_id` in front matter do, which is the right place
for a judgement a human made rather than something any provider reported.

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

Title, platform, status, dates, rating, and `draft` are editable, through "Edit game info" in the
interactive flow or via `gamelog review`. `retroachievements_id`/`steam_appid` are deliberately
not — relinking a provider ID is a more consequential action than a date fix, and isn't
implemented yet.

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

"Add a planned replay" (always offered in the interactive flow) creates one; it refuses a second
placeholder for the same effective platform, the same "don't fragment into two bookmarks for one
intent" reasoning as the one-shot statuses' per-platform cap, though `planned` isn't itself part of
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
suggestion, then enter the dates yourself through the normal interactive flow above.

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

## `gamelog scan` — find games you haven't logged

`gamelog scan` lists games on RetroAchievements and Steam that have no entry in
`content/games`, then offers a multi-select to create entries for the ones you pick.

```
go run ./cmd/gamelog scan                    # Steam games with >=5h playtime
go run ./cmd/gamelog scan --min-hours 20     # narrower
```

Matching is by external ID where one is set, falling back to a normalised title — so a game
logged before those fields existed is still recognised, and casing or a trailing `®` doesn't
produce a duplicate (`elden-ring` matches `ELDEN RING`).

Anything created is written with `draft: true`, because every field is inferred. Review it and
flip the flag to publish. Nothing is written without an explicit selection.

Scanning uses only the two bulk endpoints (one request each), so it's fast and can't be
rate-limited, but those endpoints carry no start dates. Create the entry, then run
`gamelog suggest <slug>` for the precise range.

A RetroAchievements game counts as finished when it carries a real award — the report names
which one, since `12/189 achievements → finished` only makes sense once you can see it was
`beaten-hardcore` rather than a mastery. A `mastered`/`completed` award — every achievement, not
just the ones needed to beat it — is suggested as `status: mastered` instead of `finished`.
**Steam games are never auto-marked finished**: playtime alone says nothing about completion.

## `gamelog review` — triage the draft backlog

`scan` always writes new entries with `draft: true`. `gamelog review` is how you work through
them: it walks every draft game one at a time and asks what to do with it.

```
go run ./cmd/gamelog review
```

Each game shows a summary card — title, provider links, current status/dates, and its logged
playthroughs — followed by a choice:

- **Publish as-is** — flips `draft: false`, nothing else changes.
- **Edit, then publish** — opens the same form as "Edit game info," then publishes regardless of
  what its own `draft` toggle was left at.
- **Edit without publishing** — same form, but `draft` stays whatever the form set it to, for
  partial progress you'll come back to.
- **Mark dropped & publish** — sets `status: dropped` and publishes.
- **Skip** — leaves it draft and moves on; it's excluded for the rest of *this* run, but
  `gamelog review` re-filters `draft: true` fresh every time you run it, so nothing skipped is
  lost — it just shows up again next time.
- **Delete this stub** — only offered when the game has no logged playthroughs *and* no archive
  record for either linked provider ID. Once either exists, this isn't a plausible false-positive
  scan match anymore, so the option disappears rather than risking real data.
- **Quit** — stops the loop; everything not yet acted on stays `draft: true`.

## `gamelog stale` — nudge for games that have gone quiet

Steam's `GetOwnedGames` reports `rtime_last_played` per game, captured into every Steam archive
record as `last_played`. `gamelog stale` uses it to find games you've probably stopped playing but
never marked as such: currently `status: playing`, Steam-linked, and untouched for a while — draft
or published. That makes it a fast way to pre-classify the `scan`-created draft backlog by
achievement completion before doing a full `gamelog review` pass on it; cards for draft games are
tagged `[draft]` so it's clear accepting one doesn't publish it.

```
go run ./cmd/gamelog stale                # 30+ days since last played
go run ./cmd/gamelog stale --days 60      # narrower
```

For each one it computes a guess — **mastered** at 100% achievement completion, **finished** at
≥90%, **dropped** otherwise, or **dropped at low confidence** when the game has no achievements to
go on at all (playtime alone doesn't prove completion, so "dropped" is the safer default, not a
claim) — and shows it alongside the last-played date, achievement count, and playtime. Nothing is
ever written automatically; you choose:

- **Correct — mark _status_** — applies the suggested status as-is.
- **Nope — mark _status_ instead** — one for each of the other statuses `stale` might have
  guessed (mastered/finished/dropped), so correcting a wrong guess doesn't require opening the
  full edit form just to change one field.
- **Edit before applying** — opens "Edit game info," prefilled with the suggested status, so you
  can change it, add a finish date, or adjust anything else before confirming.
- **Still playing / skip for now** — leaves it untouched. There's no persistent dismissal — if it's
  still `playing` and still stale, the next `gamelog stale` run will surface it again.
- **Quit** — stops the loop.

None of the one-shot statuses (`ongoing`, `multiplayer`, `software`) ever shows up here —
they aren't `status: playing` by definition (see above), and something used in indefinite session
bursts has no "done" to detect from staleness alone. Same reasoning `gamelog scan` already applies to Steam elsewhere: a completion signal comes
from achievements or an explicit human decision, never from playtime or silence by itself.

## `gamelog close` — cap playthroughs left open

`gamelog close` finds every playthrough whose trailing date is blank and offers to cap it at the
last day it was actually played. **It never changes a status** — the only thing it writes is a
date.

This is a different question from the one `gamelog stale` asks, which is why it's a different
command. `stale` looks at games still marked `playing` and guesses a *terminal status* from
achievement completion. The entries `close` targets are mostly one-shot games whose status is
already correct; the only thing wrong with them is a trailing `finished: ""` that renders as
"ongoing" forever. A sandbox you last touched in 2017 isn't mid-playthrough, but it isn't
"finished" either. Closing the date and picking a status are separate decisions.

The whole list is presented as **one pre-selected multi-select**, not a game-at-a-time loop:
"cap it at the last day I played" is right for most of them, so the cheap interaction is
accepting in bulk and deselecting the exceptions. It's filterable — type to narrow the list.

The closing date comes from the archive's `last_played` when there is one, falling back to the
open session's own `started` date. The fallback is the right answer more often than it looks: a
party game played on one evening has a start date and nothing else, and that evening *is* when it
stopped. Each row shows which source it used, so a wrong date is visible before you confirm.

Excluded by default:

- **`playing` games** — an open date is correct there. `--all` includes them.
- **`paused` games, always** — `--all` does not reach these. Paused means on hold, not abandoned;
  it's the one status `gamelog stale` deliberately refuses to date-stamp, and an open-ended bar is
  the honest render for something you intend to resume.
- **Anything with no date anywhere.** Inventing one would be worse than leaving it open.

Writes go through the same pre-write proof as every other playthrough write: the pending rewrite is
diffed against what's on disk and refused if a field would vanish. An entry that was closed between
building the list and confirming it is left alone rather than overwritten.

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
```

The interactive "new game" form accepts either the bare number or the full URL you copied it
from (`retroachievements.org/game/4650`, `store.steampowered.com/app/1145360/Hades/`) and
stores the bare ID. `suggest` accepts a pasted URL in front matter too.

Everything that walks a game's archive takes `[]providerLink` (from `Doc.ProviderLinks` or
`GameSummary.ProviderLinks`) rather than a positional `(raID, steamAppID)` pair, so a fourth
provider is a client file plus one entry in `providerOrder` — not an edit to every signature
that touches the archive.

### PlayStation, via Exophase

**Sony has no public API.** Trophy history is read from a public [Exophase](https://www.exophase.com)
profile instead, which mirrors PSN including per-trophy unlock timestamps — the part actually
worth archiving. The alternative is reverse-engineering Sony's endpoints and holding an NPSSO
cookie that expires every couple of months; that can be added later behind the same
`providerPSN` key, and `Source` on the record exists so the two are distinguishable.

`psn_id` is the `NPWR…` trophy-set ID, which is Sony's and permanent — Exophase reports it as
`canonical_id`. The archive keys on that, not on anything Exophase owns, so the records survive
the mirror going away.

There are no secrets involved: `EXOPHASE_PSN_USER` is a public profile name, and the profile
just has to be public. Records are written with `source: exophase` and are merged by exactly
the same never-lose rules as every other provider — a private profile or a failed fetch updates
`last_attempt`/`last_error` and leaves the history alone.

Three quirks, all found against live responses:

- Cloudflare serves an interstitial to Go's default `Go-http-client/1.1` User-Agent. Requests
  must send a browser one (`exophaseUserAgent`) — the same class of trap as RA's 403.
- **The JSON API's per-game `meta` omits `canonical_id`.** The `NPWR…` ID only appears in the
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
| `EXOPHASE_PSN_USER` | Your Exophase profile name, for PlayStation trophies. **Not a secret** — it's a public profile, which just has to be public. |

Steam's achievement/playtime endpoints only return data for a public profile, or your own
profile when the key you're using belongs to that account. Note that per-game achievement
privacy is separate from profile privacy, so an otherwise-public account can still return
"profile is not public" for one game.

### Notes for future maintenance

Both APIs have sharp edges that aren't obvious from their docs, and each one silently produced
wrong or missing output before it was found by running against live data:

- RetroAchievements returns **403 for Go's default `Go-http-client/1.1` User-Agent.** Requests
  must send a descriptive one (`raUserAgent`).
- `API_GetGameInfoAndUserProgress` omits `HighestAwardKind`/`HighestAwardDate` unless **`a=1`**
  is passed — without it every suggestion silently degrades to medium confidence.
- That award date is **RFC3339**, while the per-achievement `DateEarned` fields are zoneless
  SQL datetimes. They need separate parsers (`retroachievements.ParseRAAwardDate` vs `retroachievements.ParseRADate`).
- An unknown RA game ID returns **HTTP 200 with a bare `[]`**, not a 404.
- Steam returns **400/401/403 with a useful JSON body** for its expected "no data" cases, so
  the body matters more than the status.
- Steam's API only accepts a 17-digit SteamID64; a vanity name must go through
  `ISteamUser/ResolveVanityURL` first.
- Exophase 403s Go's default User-Agent (Cloudflare), omits `canonical_id` from its JSON API,
  and only exposes per-service player ids on the account page. See "PlayStation, via Exophase".

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
