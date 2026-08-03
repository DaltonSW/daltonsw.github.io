# gamelog

A terminal tool for maintaining this site's `content/games/<slug>/_index.md` playthrough log.

Run with no arguments for the interactive flow: pick or create a game, edit its info, log a new
playthrough, log a new session, or update an existing playthrough (finish date, status,
rating, notes). It loops — "Do another?" after each action — so a sitting can cover several
games without relaunching.

```
go run .          # interactive
go run . review   # walk through draft games one at a time
go run . help     # usage summary
```

## Where things live

Three layers, deliberately separate, because they have different owners and different rules:

```
content/games/<slug>/
  _index.md          authored by hand. The tool writes it at creation, and can edit
                     select fields afterwards (see "How front matter is edited")
                     without ever touching the markdown body below it.
  playthroughs.yaml  tool-owned. Encoded whole on every write.

archive/
  retroachievements/4650.json    captured from the API. Merge-on-write, never
  steam/1145360.json             hand-edited, never read by Hugo.
```

**The archive is keyed by provider ID, not by slug, and lives outside `content/`.** A slug is a
presentation decision — it's the URL. When the archive was stored under one, renaming a game
moved it and deleting a game destroyed unlock history that may be impossible to re-fetch. Provider
IDs never change. Being outside the content tree also means Hugo never parses `raw`, and never
publishes it: bundle resources are copied to the built site, so the old layout was serving every
captured API response publicly.

A game on both services gets two archive files. Nothing in the archive links them —
`retroachievements_id` and `steam_appid` in front matter do, which is the right place for a
judgement a human made rather than something either provider reported.

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
- **`confirmAndWrite` re-encodes the pending result and diffs it against the bytes on disk.**
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

`status: session-based` marks a game (roguelikes, multiplayer) that doesn't have a meaningful
start/finish narrative — it's played in an open-ended series of sessions with no state that ends
play. Such a game gets exactly one `playthroughs.yaml` entry, ever, whose `sessions:` list *is*
the record: once it has that one entry, "Start a new playthrough" stops being offered for it, so
there's nothing to accidentally fragment into a second discrete playthrough later.

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
go run . suggest okami        # direct, by slug
go run . suggest              # interactive game picker
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
go run . scan                    # Steam games with >=5h playtime
go run . scan --min-hours 20     # narrower
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
go run . review
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
never marked as such: published, currently `status: playing`, Steam-linked, and untouched for a
while.

```
go run . stale                # 30+ days since last played
go run . stale --days 60      # narrower
```

For each one it computes a guess — **mastered** at 100% achievement completion, **finished** at
≥90%, **dropped** otherwise, or **dropped at low confidence** when the game has no achievements to
go on at all (playtime alone doesn't prove completion, so "dropped" is the safer default, not a
claim) — and shows it alongside the last-played date, achievement count, and playtime. Nothing is
ever written automatically; you choose:

- **Accept** — applies the suggested status as-is.
- **Edit before applying** — opens "Edit game info," prefilled with the suggested status, so you
  can change it, add a finish date, or adjust anything else before confirming.
- **Still playing / skip for now** — leaves it untouched. There's no persistent dismissal — if it's
  still `playing` and still stale, the next `gamelog stale` run will surface it again.
- **Quit** — stops the loop.

A game marked `session-based` never shows up here — it isn't `status: playing` by definition (see
above), and games played in indefinite session bursts have no "done" to detect from staleness
alone. Same reasoning `gamelog scan` already applies to Steam elsewhere: a completion signal comes
from achievements or an explicit human decision, never from playtime or silence by itself.

## `gamelog achievements` — full unlock history

`gamelog achievements [slug]` writes every achievement, with its unlock timestamp, to
`archive/<provider>/<id>.json` — one file per provider, resolved from the game's
`retroachievements_id`/`steam_appid`. `scan` does this automatically for each entry it creates;
run the command directly to refresh a game or backfill one that predates it.

```
go run . achievements spelunky
```

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

Add either or both to a game's front matter (blank/omitted is fine — that provider is just
skipped):

```yaml
retroachievements_id: 4650      # numeric ID from the game's retroachievements.org URL
steam_appid: 1145360            # numeric appid from the game's Steam store URL
```

The interactive "new game" form accepts either the bare number or the full URL you copied it
from (`retroachievements.org/game/4650`, `store.steampowered.com/app/1145360/Hades/`) and
stores the bare ID. `suggest` accepts a pasted URL in front matter too.

### Credentials

Copy `.env.example` to `.env` (gitignored) and fill it in. `suggest` finds that file whether
it's run from this directory or the repo root. Real environment variables always take
precedence, so `RA_API_KEY=... go run . suggest x` still overrides the file.

| Variable | What it is |
|---|---|
| `RA_USERNAME` | Your RetroAchievements username |
| `RA_API_KEY` | RetroAchievements API key, from your account settings on retroachievements.org |
| `STEAM_API_KEY` | Steam Web API key, from steamcommunity.com/dev/apikey |
| `STEAM_ID` | Your SteamID64 **or** the vanity name from your profile URL — a vanity name is resolved automatically. `STEAM_USER_ID` is accepted as an alias. |

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
  SQL datetimes. They need separate parsers (`parseRAAwardDate` vs `parseRADate`).
- An unknown RA game ID returns **HTTP 200 with a bare `[]`**, not a 404.
- Steam returns **400/401/403 with a useful JSON body** for its expected "no data" cases, so
  the body matters more than the status.
- Steam's API only accepts a 17-digit SteamID64; a vanity name must go through
  `ISteamUser/ResolveVanityURL` first.

The committed `testdata/*.json` fixtures were corrected against live responses — keep them that
way, since a fixture written from the documentation is what let all of the above pass tests
while being broken in practice.

## `gamelog project` — achievement counts for the site

The archive lives outside `content/` on purpose (see above), so Hugo can never read it directly.
`gamelog project` is the projection step: it walks every game with a `retroachievements_id` or
`steam_appid`, sums that game's `unlocked`/`total` across whichever providers it's linked to (RA
12/40 + Steam 46/54 → 58/94), and writes the result to
`content/games/<slug>/achievement-summary.yaml` — no `raw`, no per-achievement list, just the two
numbers the games list actually displays.

```
go run . project
```

It only ever reads what's already in `archive/` — **no API calls**, so it's safe and fast to
re-run any time the projection needs rebuilding (after `gamelog achievements` refreshes a game, or
after the summary's shape changes) without touching credentials or rate limits. `gamelog scan` and
`gamelog achievements` already call the same write for whichever game they just touched, so a
full `project` run is only needed for a bulk backfill or to pick up an archive edited by hand.

A game with nothing archived for either provider gets no file — not a `0/0` one — and any stale
file left over from a cleared or relinked provider ID is removed rather than shown. The games list
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
