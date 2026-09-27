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
  that directory) plus a few CLI commands. Its README documents the storage layout and each
  provider's API quirks that are easy to regress — read it before touching any client.
- **Storage is split three ways, on purpose.** `content/games/<slug>/_index.md` is authored by
  hand; the tool creates it and afterwards only splices individual front-matter fields, never the
  markdown body; `playthroughs.yaml` beside it is tool-owned and
  rewritten whole; `archive/<provider>/<id>.json` holds captured API history, keyed by IDs that
  never change and kept outside `content/` so a rename or delete can't destroy it — and so Hugo
  never parses or publishes it. The site reads captured data only through tool-generated
  projections beside `_index.md` (`achievement-summary.yaml`, plus `campaigns.yaml` and
  `btd6-summary.yaml` for the two non-achievement providers), which are regenerable from `archive/`
  and never published either.
- **`tools/gamelog/KNOWN-ISSUES.md` lists what's outstanding and, just as importantly, the
  invariants not to undo.** Read it before editing `internal/model/playthroughs.go` or
  `internal/model/archive.go` — the inline `Extra` catch-all, the string-typed dates, and the
  pre-write loss check each prevent a specific silent data loss that has already happened once.
- Credentials live in `tools/gamelog/.env` (gitignored). `.env.example` is the template.

## Working style

- Verify assumptions about external APIs against live responses, not documentation. This
  codebase has already shipped a fully broken integration behind a green test suite because
  fixtures were written from docs.
- Don't build tooling for one-off work. One-time data gathering is better done directly than by
  adding a feature that runs once.
