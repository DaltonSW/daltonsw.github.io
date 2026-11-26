# daltonsw.github.io

Personal site (Hugo). The GameLog is the part with real design pressure on it.

## GameLog intent

**The goal is a complete, durable archive of gameplay history — not a particular presentation
of it.** Capture as much as the sources will give, keep it in one place, and treat how it's
displayed as a separate, changeable concern.

Consequences that should drive design decisions:

- **Capture generously.** If an API returns a field, there should be a good reason not to keep
  it. Storage is cheap; re-fetching history that's been delisted or lost access to may be
  impossible.
- **Never lose captured data.** A refresh must not be able to shrink the archive. If a provider
  returns less than last time (rate limit, delisting, revoked access, private profile), the
  previously captured data wins. Prefer merge/union over overwrite.
- **Display will change repeatedly.** Expect to try several timeline/summary formats and discard
  some. Data should be shaped for querying, not for one layout. Iterating on presentation must
  never require re-fetching.
- **Volume is the point.** ~200+ games and thousands of achievements is expected and desirable.
  Don't propose trimming the dataset for tidiness.

## Practical notes

- Run `npm install` before `hugo server` on a fresh clone — the timeline depends on
  `vis-timeline` from `node_modules`, mounted into `assets/` by `config/_default/hugo.toml`.
- `tools/gamelog/` is the Go tool that maintains the game log: a local web UI (`go run .` from
  that directory). Its README documents the storage layout and each
  provider's API quirks that are easy to regress — read it before touching any client.
- **Storage is split three ways, on purpose.** `content/games/<slug>/_index.md` is authored by
  hand; the tool creates it and afterwards only splices individual front-matter fields, never the
  markdown body, and carries no dates (started/first finished/last played derive from playthroughs,
  mastered from achievements). Its `status:` is written by the tool from the game's runs, except
  for one-shot games and games with no runs; `playthroughs.yaml` beside it is tool-owned and
  rewritten whole; `archive/<provider>/<id>.json` holds captured API history, keyed by IDs that
  never change and kept outside `content/` so a rename or delete can't destroy it — and so Hugo
  never parses or publishes it. The site reads captured data only through tool-generated
  projections beside `_index.md` (`achievement-summary.yaml`, plus `campaigns.yaml` and
  `btd6-summary.yaml` for the two non-achievement providers, and `stats-summary.yaml` for Steam's
  per-game stats), which are regenerable from `archive/` and never published either.
- **Curated lists** (`/games/lists/`) live in `data/game-lists.yaml`: authored through the tool's
  Lists page, order is rank, games by slug and achievements by provider + record id + key.
- Credentials live in `tools/gamelog/.env` (gitignored). `.env.example` is the template.

## GameLog: don't undo these

Each prevents a silent data loss that already happened once. Before changing the code behind one,
read the README section it names; the code comments carry the details. API quirks per provider
(User-Agent 403s, undocumented params, key choices) are in the README, so read the provider's
section before touching its client.

- **Refreshes merge, never overwrite.** `mergeProvider` / `mergeActivity` in
  `internal/model/archive.go` only grow the archive: a fetch that errors or comes back empty leaves
  achievements alone, and activity fields merge by rules that can't shrink them (max, latest,
  fill-blank). The regression tests in `archive_test.go` cover this; keep them.
- **Mastery milestones are only added** (`ProviderRecord.Mastery`, `unionMastery`). A DLC or an RA
  set revision can make a mastery stop being derivable, so never recompute it from the current list.
- **`Extra map[string]any` with `yaml:",inline"`** on `PlaythroughEntry`, `SessionEntry` and
  `FrontMatter` is what keeps a hand-added field from vanishing on the next whole-file rewrite.
- **Dates in `playthroughs.yaml` are `string`s.** An unquoted `2026-01-04` is a YAML timestamp, and
  re-encoding it rewrites every date in the file.
- **Every run's dates are a `sessions:` list.** Nothing writes a flat `started`/`finished`; a
  hand-typed flat pair is moved into one session on load. The latest session is the one with the
  greatest `started`, never the last by position.
- **Every playthrough write goes through `mutate.VerifyNoLoss`** (via `WriteEntry` /
  `WriteFrontMatter`). If a new operation trips it, the operation is wrong: declare the paths it
  legitimately removes, never widen the allowlist to silence it.
- **Playthrough writes are atomic** (temp file + rename in `PlaythroughsFile.Save`).
- **Nothing removes an entry from `playthroughs[]` by index.** `SplitPlaythrough` appends at the
  end so no existing entry's path ever shifts.
- **The archive is keyed by provider ID, not slug, and lives outside `content/`.** Slugs change and
  everything in `content/` gets published.
- **Status flows from runs to the game, never back** (`model.DeriveGameStatus`,
  `mutate.SyncGameStatus`). Don't edit a derived game status directly; change a run. A game's front
  matter carries no dates, so don't reintroduce `started`/`finished` on `FrontMatter`.
- **Playthrough boundaries are confirmed by hand**, never derived from achievement gaps.
- **Account-wide Catch up loads only.** A source loaded on its own is never recorded as a run
  (`RecordRun` moves the "new since" boundary for every source). Provider clients are shared per
  process (`internal/commands/clients.go`); don't build one per game, and guard any client state
  with a mutex.
- **Nadeo credentials belong to a real player's account.** Never make live Trackmania requests
  while developing; the user runs those.
- **Tests never reach the internet.** Packages that make HTTP calls run through
  `testutil.RunOffline`; stub the endpoint it names rather than loosening the guard.

## Working style

- Verify assumptions about external APIs against live responses, not documentation. This
  codebase has already shipped a fully broken integration behind a green test suite because
  fixtures were written from docs.
- Don't build tooling for one-off work. One-time data gathering is better done directly than by
  adding a feature that runs once.
