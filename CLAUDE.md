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
- `tools/gamelog/` is the Go CLI that maintains `content/games/`. Its README documents the
  RetroAchievements/Steam API quirks that are easy to regress — read it before touching either
  client.
- **`tools/gamelog/KNOWN-ISSUES.md` lists outstanding defects with runnable reproductions.** Two
  of them silently destroy or misattribute front-matter data. Read it before editing
  `patch.go`, `frontmatter.go`, or `games.go:Slugify`.
- Credentials live in `tools/gamelog/.env` (gitignored). `.env.example` is the template.

## Working style

- Verify assumptions about external APIs against live responses, not documentation. This
  codebase has already shipped a fully broken integration behind a green test suite because
  fixtures were written from docs.
- Don't build tooling for one-off work. One-time data gathering is better done directly than by
  adding a feature that runs once.
