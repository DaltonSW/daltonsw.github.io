# GameLog playthrough splits

## Applied 2026-07-28

All 36 games below were applied as edited. Notable departures from what I originally proposed:

- **Baldur's Gate 3** and **Dark Souls III** split into three playthroughs, not two — you added a
  second marker on each. DARK SOULS III's first (2018) entry is `dropped`, matching the "bounced
  off it" prose already in `_index.md`; the other two stayed `finished`.
- **Super Meat Boy**'s trailing `2024-04-07` session is gone — you deleted it outright rather than
  keeping it in either half.
- **Terraria**, **Stardew Valley**, **Civ V**, and **Civ VI** got split far finer than proposed —
  Stardew and both Civs now have one playthrough per session (five, four, and four respectively),
  all `playing`. Terraria also picked up a session merge: `2022-01-05..2022-01-06` and
  `2022-01-23..2022-02-23` collapsed into one `2022-01-05..2022-02-23` session before the split.
- **Phoenix Wright** and **Comet 64** deferred — markers removed, no split applied, sessions
  unchanged.
- **A Hat in Time**, **Murder by Numbers**, and **Omori** deferred the split but the comments you
  left became `notes:` fields on the (unsplit) entry.
- **Cuphead** and **Portal 2** split as proposed, with your inline comments carried into `notes:`
  on the relevant new entries (DLC timing for Cuphead; "probably multiplayer" on Portal 2's second
  and third entries).
- Every other game split exactly as proposed, one marker per split, status carried over from the
  original entry.

Verified with the tool's real YAML decoder (`LoadPlaythroughs` on all 36 files) and a full
cross-playthrough overlap scan — no parse errors, no new overlaps. The only overlaps left in the
corpus are Sekiro and Spelunky 2's PS4 entries, already flagged in `gamelog-sessions.md` and
unrelated to this pass.

---

This is a different question from `gamelog-sessions.md`. That doc fixed *dates* — a session's
`finished` field getting stretched by a stale relaunch. This doc is about *shape*: every game
below has all of its play recorded as ONE playthrough entry with a gap of 6+ months or more
between two of its sessions. The timeline draws a dim connector bar across every gap inside a
single playthrough entry, on purpose — it's supposed to mean "paused, then resumed the same run."
For a gap this long, that's not guaranteed. It's just as likely you dropped the save and started
over, in which case this should be two separate playthrough entries with nothing connecting them.

The archive can't tell these apart — a fresh New Game and a resumed save produce identical data
(an achievement, a date). Only you know which one happened. This list exists because
`tools/gamelog/KNOWN-ISSUES.md` (issue 7) flagged the general problem months ago and deferred it
for exactly this reason; this is that list, filled in with real dates and current data, for every
finished/mastered/dropped game with a qualifying gap, plus a separate section for `session-based`
sim/builder games where the same question applies for a different reason (see below).

Left out entirely: roguelikes and meta-progression games (Hades, Slay the Spire, Risk of Rain,
Spelunky, Monster Train, Enter the Gungeon, Balatro, Peglin, Dead Cells, The Binding of Isaac:
Rebirth) and true live-service/multiplayer games (TF2, Bloons TD6, Golf With Your Friends,
MultiVersus, Jackbox). Neither has a save file to restart — a roguelike's meta-progress carries
forward no matter how long the gap, and multiplayer has no "playthrough" to resume in the first
place. For those, picking the game back up unambiguously continues the same one, so the dim bar
is already correct.

## How to edit this

Each block shows the game's current session list. Where I found a plausible split point, there's
a `↑ same playthrough / ↓ new playthrough?` marker between two sessions.

- **Delete the marker line** — *defer.* Keep it as one playthrough; the dim bar stays and is
  taken as correct (this really was the same save, paused and resumed).
- **Leave the marker in place** — apply the split. The sessions above it become playthrough A
  (status carries over, or set your own if the abandoned-then-restarted shape means the first
  chunk should be `dropped` while the second is `finished`/`mastered`), the sessions below become
  a new playthrough B.
- **Move the marker** if the split point isn't where I put it, or **add a note** with the real
  status/platform for each half if it splits unevenly.

**The session lines themselves are editable too**, same as `gamelog-sessions.md` — change a date,
delete a session that shouldn't be there, add one that's missing, whatever's wrong. When you hand
this back I'll treat every block's session list as the full intended truth for that game (split or
not) and write it whole, not just act on the marker. One pass covers both the split-vs-resume
call and any date cleanup for these games — no need to touch `gamelog-sessions.md` for anything
that's already shown here. None of the games below overlap with what's still open in that file
(FTL, Jackbox 3, Sekiro/Spelunky 2's PS4 entries), so there's no conflict either way.

Hand the file back and I'll apply whatever's left standing.

---

## Strong candidates

These have something beyond just "long gap" pointing at a restart.

### Baldur's Gate 3
`baldur-s-gate-3` · 431.0h · 45/54 · finished

```
2023-10-15..2023-11-11
2023-12-02..2023-12-03
2023-12-31..2024-02-11
↑ same playthrough / ↓ new playthrough?
2024-11-02..2024-12-25
↑ same playthrough / ↓ new playthrough?
2025-01-14..2025-01-18
2025-01-29..2025-02-04
2025-02-15..2025-03-04
```

`tools/gamelog/KNOWN-ISSUES.md` quotes you directly on this one: "431 hours and 45/54
achievements in one entry spanning 2023-10 → 2025-03; the owner confirms that is at least two
separate campaigns." This is that confirmation, finally acted on. 265-day gap at the marker;
double-check the split point matches where the second campaign actually started.

### DARK SOULS™ III
`dark-souls-iii` · 49.1h · 30/43 · finished

```
2018-05-21..2018-05-21
↑ same playthrough / ↓ new playthrough?
2021-05-01..2021-05-15
↑ same playthrough / ↓ new playthrough?
2026-04-16..2026-04-16
```

The `_index.md` prose already says the 2018 session was "tried at roommate's request, bounced off
of it after trying Iudex Gundyr" — that reads as an abandoned attempt, not the start of the run
that finished in 2021. If so, the 2018 session is arguably its own one-day `dropped` playthrough,
separate from the real one.

## Other candidates

Sorted by gap length, longest first. Same format — pick the split point, leave or delete the
marker.

### Portal
`portal` · 11.2h · 12/15 · finished

```
2012-08-12..2012-08-13
2012-09-30..2012-10-21
↑ same playthrough / ↓ new playthrough?
2026-01-07..2026-01-10
```

4826-day gap — the largest in the corpus. The 2012 sessions beat the game (`Heartbreaker`); the
2026 session is entirely advanced-map achievements (`Cupcake`, `Fruitcake`, `Vanilla Crazy Cake`).
Reads like a genuine second trip back for the achievements you skipped, which fits either model —
same save resumed, or a fresh save just to mop up chambers. Your call which this was.

### Shovel Knight: Treasure Trove
`shovel-knight-treasure-trove` · 38.6h · 32/138 · finished

```
2014-08-03..2014-08-25
↑ same playthrough / ↓ new playthrough?
2022-01-03..2022-01-03
↑ same playthrough / ↓ new playthrough?
2024-09-17..2024-10-05
```

2688-day gap, the second-largest here. 138 achievements total (this bundles several campaigns —
worth checking whether the post-gap sessions are even the same sub-game as 2014.

### Super Meat Boy
`super-meat-boy` · 26.6h · 29/48 · finished

```
2012-11-17..2012-11-18
2013-10-07..2013-10-07
↑ same playthrough / ↓ new playthrough?
2021-01-09..2021-01-15
```

2651-day gap. This one already went through the 2026-07-27 session-date pass (the 2024-04-07 tail
was trimmed off a stale `..2024-04-07` stretch) — this is a step up from that: whether 2013 and
2021 are the same attempt at all.

### Dishonored
`dishonored` · 159.3h · 26/80 · finished

```
2016-06-18..2016-06-25
2016-08-19..2016-08-19
↑ same playthrough / ↓ new playthrough?
2021-05-18..2021-05-18
```

1733-day gap. `KNOWN-ISSUES.md` already names Dishonored's "high playtime is speedrun practice
rather than story progress" — worth a `notes:` field either way, split or not.

### Fallout: New Vegas
`fallout-new-vegas` · 68.7h · 26/75 · finished

```
2013-04-23..2013-04-23
↑ same playthrough / ↓ new playthrough?
2018-02-25..2018-03-08
↑ same playthrough / ↓ new playthrough?
2022-10-29..2022-11-06
```

Two candidate gaps (1769d, then another 1696d after that session). Possibly three playthroughs,
not two — check whether 2018 and 2022 belong together before accepting a single split.

### XCOM: Enemy Unknown
`xcom-enemy-unknown` · 17.0h · 37/85 · dropped

```
2013-12-21..2013-12-21
↑ same playthrough / ↓ new playthrough?
2018-10-07..2018-10-13
```

1751-day gap. XCOM campaigns are permadeath — resuming five years later almost certainly means a
fresh campaign, not the same run. Low stakes either way since it's `dropped`.

### Borderlands: The Pre-Sequel
`borderlands-the-pre-sequel` · 17.4h · 13/63 · dropped

```
2015-06-15..2015-06-17
↑ same playthrough / ↓ new playthrough?
2019-09-20..2019-10-05
```

1556-day gap, `dropped` status. Low stakes, quick to decide.

### The Talos Principle
`the-talos-principle` · 5.7h · 9/40 · dropped

```
2019-10-29..2019-11-01
↑ same playthrough / ↓ new playthrough?
2023-06-19..2023-06-19
```

1326-day gap, `dropped`. Single day on either side of the gap — probably an easy call.

### Ai: The Somnium Files
`ai-the-somnium-files` · 33.2h · 45/45 · mastered

```
2020-06-29..2020-07-05
↑ same playthrough / ↓ new playthrough?
2024-09-20..2024-09-20
```

1538-day gap. Already `mastered` by the end of the first session block — the 2024 day looks more
like a relaunch than new progress; worth checking whether it even unlocked anything new before
deciding this is a playthrough split at all rather than a `gamelog-sessions.md`-style trim.

### Inscryption
`inscryption` · 19.1h · 23/40 · finished

```
2022-01-14..2022-02-05
↑ same playthrough / ↓ new playthrough?
2025-02-17..2025-02-17
```

1108-day gap.

### Hollow Knight
`hollow-knight` · 273.1h · 63/63 · mastered

```
2017-03-25..2017-04-30
2017-08-07..2017-08-07
2017-10-06..2017-10-06
↑ same playthrough / ↓ new playthrough?
2020-12-16..2020-12-19
2022-04-21..2022-05-01
2024-08-11..2024-08-11
2026-05-02..2026-05-02
```

Largest total playtime on this list, three more 6mo+ gaps after the marked one (488d, 833d, 629d)
— if this splits at all it might be three or four playthroughs, not two. Metroidvania with
persistent save-file completion, so "same playthrough resumed" is very plausible; flagged mainly
because of scale, not strong restart evidence.

### Crash Bandicoot N. Sane Trilogy
`crash-bandicoot-n-sane-trilogy` · 22.4h · 48/74 · finished

```
2019-08-06..2019-08-14
↑ same playthrough / ↓ new playthrough?
2022-05-14..2022-05-14
```

1004-day gap.

### Borderlands 2
`borderlands-2` · 76.7h · 35/75 · finished

```
2015-02-15..2015-02-15
2015-03-19..2015-03-19
2015-06-10..2015-06-21
↑ same playthrough / ↓ new playthrough?
2017-12-22..2017-12-22
```

915-day gap.

### Fallout 4
`fallout-4` · 53.7h · 29/84 · finished

```
2015-11-12..2015-11-18
↑ same playthrough / ↓ new playthrough?
2018-03-13..2018-04-09
```

846-day gap.

### Evoland
`evoland` · 18.0h · 27/29 · finished

```
2013-11-27..2013-11-27
↑ same playthrough / ↓ new playthrough?
2016-04-23..2016-05-04
↑ same playthrough / ↓ new playthrough?
2017-01-31..2017-01-31
```

878-day gap. Already has prose notes in `_index.md` from the 2026-07-27 pass — check those before
deciding, they may already answer this.

### Fez
`fez` · 12.1h · 7/12 · finished

```
2013-09-08..2013-09-08
2013-09-21..2013-09-21
↑ same playthrough / ↓ new playthrough?
2015-11-03..2015-11-03
```

773-day gap. Only 12 achievements total — same low-signal caveat as Disco Elysium in the sessions
doc; a single day on each side doesn't say much either way.

### Portal 2
`portal-2` · 27.5h · 34/51 · finished

```
2012-11-17..2012-11-24
2012-12-31..2012-12-31
↑ same playthrough / ↓ new playthrough?
2014-06-05..2014-06-10 # This one was probably a multiplayer playthrough
↑ same playthrough / ↓ new playthrough?
2016-07-11..2016-07-11 # This one was probably a multiplayer playthrough
```

762-day gap, another 762-day gap after that (2014-06-10 → 2016-07-11) — possibly three
playthroughs.

### Dark Souls: Remastered
`dark-souls-remastered` · 113.3h · 22/41 · finished

```
2019-10-28..2019-10-28
↑ same playthrough / ↓ new playthrough?
2020-05-07..2020-06-04
↑ same playthrough / ↓ new playthrough?
2021-03-23..2021-03-23
↑ same playthrough / ↓ new playthrough?
2021-06-28..2021-06-28
↑ same playthrough / ↓ new playthrough?
2026-04-26..2026-04-28
```

1763-day gap to a single new achievement in 2026 — this is the one you originally flagged. Data
checked out fine in `gamelog-sessions.md` (the session dates match real unlocks), but that's a
different question from whether it's the same save. A single achievement after five years reads
more like "one more trophy" than "started over," but only you know if that save still existed.

### Phoenix Wright: Ace Attorney Trilogy
`phoenix-wright-ace-attorney-trilogy` · 70.2h · 30/30 · mastered

```
2021-02-11..2021-03-02
2023-01-05..2023-01-08
2023-02-03..2023-02-05
2025-10-18..2025-10-21
```

986-day gap at the marker (674d gap earlier in the list too — three chunks total, possibly three
distinct cases: three games in a trilogy played separately, not a restart at all).

### Celeste
`celeste` · 51.6h · 30/32 · finished

```
2018-09-03..2018-10-01
2019-04-17..2019-04-17
↑ same playthrough / ↓ new playthrough?
2021-11-20..2021-11-20
↑ same playthrough / ↓ new playthrough?
2024-11-24..2025-01-03
```

948-day gap at the marker, another 1100-day gap after it — up to three chunks.

### Comet 64
`comet-64` · 8.4h · 13/13 · mastered

```
2024-01-20..2024-01-20
2025-02-08..2025-02-09
```

385-day gap, already `mastered` in the first session — same "was this really new progress"
question as Ai: The Somnium Files.

### Disco Elysium
`disco-elysium` · 72.8h · 13/45 · finished

```
2020-07-22..2020-08-01
↑ same playthrough / ↓ new playthrough?
2021-04-29..2021-04-29
2021-05-28..2021-05-28
2021-06-29..2021-06-30
↑ same playthrough / ↓ new playthrough?
2022-11-20..2022-11-20
```

508-day gap. Already flagged in `gamelog-sessions.md` as low-signal — 13 unlocks across 72.8
hours, so treat any proposed split here with the same skepticism.

### A Hat in Time
`a-hat-in-time` · 33.6h · 30/46 · finished

```
2017-12-25..2017-12-27
2018-09-28..2018-09-29
2019-05-20..2019-05-22
```

*Note: I think these were when DLCs or updates came out that made me go back?*

275-day gap, another 233-day gap right after — three short chunks, none over a year apart. Weakest
case on this list; may not be worth splitting at all.

### Cuphead
`cuphead` · 27.0h · 42/42 · mastered

```
2021-07-02..2021-07-08
↑ same playthrough / ↓ new playthrough?
2022-01-14..2022-01-14 # DLC release I think
2022-07-31..2022-08-09
```

190-day and 198-day gaps — borderline for the 6-month bar. Already `mastered` by end of first
session.

### Murder by Numbers
`murder-by-numbers` · 26.2h · 4/4 · mastered

```
2020-06-07..2020-06-17
2021-03-07..2021-03-10 # Achievement cleanup
```

263-day gap. Only 4 achievements total — weak signal.

### Omori
`omori` · 67.0h · 84/84 · mastered

```
2021-07-01..2021-08-07
2022-02-06..2022-02-20 # Same playthrough, went back for other route(s) and achievements
```

183-day gap — right at the threshold.

## Session-based games with separate save files

Added after a correction: I'd originally bucketed every `session-based` game in with the true
live-service/roguelike exclusions below, on the assumption that "resumed play" always means the
same save. That holds for a roguelike (Hades, Slay the Spire, Risk of Rain, Spelunky, Monster
Train, Enter the Gungeon, Balatro, Peglin) — there's no save file to abandon, meta-progress just
carries forward, so a gap is unambiguously a pause. It doesn't hold for a game built around
separate named saves or worlds. Coming back to Terraria or Stardew Valley a year later is at least
as likely to mean a new world/farm as it is picking the old one up — same problem as the
finished/mastered/dropped games above, just without a "finished" line to anchor it.

All six of these are `session-based` at the game level, which is a deliberate, tool-enforced
one-entry-forever shape (`tools/gamelog/KNOWN-ISSUES.md`: `isOneShot` in `forms.go` gates on it,
and `SyncStatus` only auto-syncs status when a game has exactly one playthrough entry). Splitting
any of these opts it out of that automatic sync permanently — a real cost on top of the usual
"is this actually a restart" judgment call, not a reason to avoid it, but worth knowing before
you do it several times over.

### Terraria
`terraria` · 196.6h · 84/137 · session-based

```
2015-08-16..2015-08-16
↑ same world / ↓ new world?
2017-01-17..2017-02-26
↑ same world / ↓ new world?
2020-05-16..2020-05-16
↑ same world / ↓ new world?
2022-01-05..2022-02-23
```

520-day gap at the marker, then two more (1175d, 599d) further down — this could easily be three
or four separate worlds, not two.

### Stardew Valley
`stardew-valley` · 210.4h · 28/49 · session-based

```
2016-03-05..2016-03-06
↑ same farm / ↓ new farm?
2019-03-01..2019-03-12
↑ same farm / ↓ new farm?
2019-08-03..2019-08-03
↑ same farm / ↓ new farm?
2022-03-22..2022-04-09
↑ same farm / ↓ new farm?
2025-11-29..2025-12-13
```

962-day gap at the marker; 1090-day gap earlier and 1330-day gap after it too — same multi-split
caveat as Terraria.

### Oxygen Not Included
`oxygen-not-included` · 71.1h · 10/51 · session-based

```
2019-09-11..2019-09-18
↑ same colony / ↓ new colony?
2024-10-11..2024-10-25
```

1850-day gap — the longest of this group. Five years is a long time to keep one asteroid running.

### Cities: Skylines
`cities-skylines` · 42.7h · 17/135 · session-based

```
2015-03-10..2015-03-11
↑ same city / ↓ new city?
2018-11-21..2018-11-21
↑ same city / ↓ new city?
2019-01-21..2019-02-02
↑ same city / ↓ new city?
2019-06-28..2019-06-29
```

1351-day gap.

### Sid Meier's Civilization V
`sid-meier-s-civilization-v` · 40.2h · 7/286 · session-based

```
2013-11-16..2013-11-16
↑ same game / ↓ new game?
2013-12-14..2013-12-14
↑ same game / ↓ new game?
2015-12-22..2015-12-22
↑ same game / ↓ new game?
2017-06-11..2017-06-20
```

738-day gap at the marker, 537-day gap after it. Civ games run one match to completion (or
abandonment) in a sitting or a few — a 4X match surviving a 2-year gap between turns would be
unusual, so this one leans toward "new game" more than the others on this list.

### Sid Meier's Civilization VI
`sid-meier-s-civilization-vi` · 59.2h · 37/320 · session-based

```
2018-05-22..2018-05-26
↑ same game / ↓ new game?
2019-02-12..2019-02-12
↑ same game / ↓ new game?
2021-07-25..2021-07-25
↑ same game / ↓ new game?
2023-05-29..2023-06-11
```

894-day gap at the marker (262d and 673d gaps elsewhere) — same reasoning as Civ V.

### Sunless Sea
`sunless-sea` · 11.6h · 5/54 · session-based

```
2017-01-07..2017-01-12
↑ same captain / ↓ new captain?
2019-05-03..2019-05-03
```

Not a builder, but the same problem for a different reason: Sunless Sea has permadeath — die and
your next voyage is a new captain by design. 841-day gap; a died-and-restarted captain is at least
as likely here as a save that just sat untouched.

## Lowest priority (dropped, short second look)

### Papers, Please
`papers-please` · 5.0h · 2/13 · dropped

```
2014-04-12..2014-04-13
↑ same playthrough / ↓ new playthrough?
2016-02-27..2016-02-27
```

685-day gap. 2/13 achievements — barely played either time.

### Red Dead Redemption 2
`red-dead-redemption-2` · 11.6h · 2/51 · dropped

```
2020-02-02..2020-02-02
↑ same playthrough / ↓ new playthrough?
2020-09-19..2020-09-19
```

230-day gap, 2/51 achievements. Barely qualifies for this list at all.

### Spyro™ Reignited Trilogy
`spyro-reignited-trilogy` · 12.2h · 28/105 · dropped

```
2019-10-16..2019-10-19
↑ same playthrough / ↓ new playthrough?
2025-08-02..2025-08-02
```

2114-day gap. Already flagged in `gamelog-sessions.md` — "All 28 unlocks are Spyro 1... worth a
look at whether this should be `dropped`." That question and this one are related: if it's staying
`dropped`, a split matters less.

## Already reviewed, not re-listed

**Outer Wilds** and **Sekiro's PC entry** both have 6mo+ gaps too, but Outer Wilds went through
`gamelog-sessions.md` already (dates confirmed, not re-litigating shape here), and Sekiro's is a
single day tacked onto an already-`mastered` entry four years later — more likely a relaunch than
a restart worth its own entry. Flagging here rather than silently dropping them: revisit if either
still looks wrong after this pass.

**TUNIC** isn't here — its second session already has a confirmed explanation in `_index.md`
("part of a multiworld randomizer, not a new playthrough"), i.e. explicitly the same-playthrough
case this doc exists to separate out.

Excluded entirely: roguelike/meta-progression games and true live-service/multiplayer titles —
listed by name near the top. Those are the only cases where a gap unambiguously just means you
weren't playing, because there's no separate save or world to have restarted instead. Every other
`session-based` game with a qualifying gap is in the "Session-based games with separate save
files" section above, not excluded — a `session-based` status means "no finish line," not "definitely the
same save."
