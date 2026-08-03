# GameLog session splits

53 sessions span 45+ days. 49 of them are wrong — an import artifact, not a real sitting. Below
is the proposed session list for each of those 49 games; the other 4 are listed at the bottom.

## New findings — 2026-07-28

You asked for a check on overlapping sessions and playthroughs with playtime landing suspiciously
long after the last real one. Same `last_played`-as-`finished` bug as the 2026-07-27 pass, but two
more games have it — they were missed the first time because that pass only walked entries shaped
like a `sessions:` list. A playthrough with exactly one session gets written collapsed
(`started`/`finished` directly on the entry, no `sessions:` wrapper at all), and neither game below
was screened against that shape until now.

**Both games below are also the only two overlaps in the whole corpus.** Each one is a second
platform entry (a PS4 replay alongside an existing PC one) whose bogus multi-year tail stretches
far enough to swallow dates the PC entry already owns. Trimming the tail to what the archive
actually supports removes the overlap as a side effect — there's no separate overlap fix needed.

### Sekiro™: Shadows Die Twice (PS4 entry)
`sekiro-shadows-die-twice` · PS4 entry: 15.1h · 28/34 · (no `status:` set)

```
2021-12-24..2021-12-24   # was ..2025-04-16
2025-04-12..2025-04-16   # new
```

Eight unlocks land on 2021-12-24 (early bosses — Gyoubu, Genichiro, Guardian Ape) and then nothing
for 1205 days. The remaining 20 unlock across 2025-04-12 → 2025-04-16, ending with `Isshin, Sword
Saint` and `Shura` — a real return trip that finished the game. `last_played` on the PSN record is
`2025-04-16`, exactly the old `finished:` date, so this is the identical bug: a relaunch date
stretched a single-day first attempt into a 1209-day session. This entry also has no `status:` —
worth setting to `mastered` while this is open, since the PC entry above it already carries that.

This PS4 session, unfixed, is what made it look like it overlapped the PC entry's 2024-07-13
session — a 1209-day span will always swallow something. There's nothing to do about the overlap
beyond this fix.

### Spelunky 2 (PS4 entry)
`spelunky-2` · PS4 entry: 14.0h · 14/32 · playing

```
2020-11-17..2020-12-03   # was ..2021-08-11
```

Every PSN unlock lands between 2020-11-17 and 2020-12-03 (gaps under 10 days throughout), then
nothing. `2021-08-11` is PSN's `last_played` — a relaunch 8 months after the last real unlock, not
a continuation. This one doesn't split into two sessions since there's no second cluster of
activity to date it against; it just needs the tail cut back to where the evidence stops. Same
overlap story as Sekiro: the bogus tail is what made this PS4 session look like it overlapped both
of the PC entry's late-2020/early-2021 sessions.

**Still open from last time, unchanged:** FTL and Jackbox 3 are still held — both fail the
playtime check against their archived unlock window and need a real date from memory, not the
archive. Not re-proposing anything for them here.

**Out of scope for both docs, worth naming so it doesn't look missed:** the games you asked
about by name — TUNIC, DARK SOULS III, DARK SOULS: REMASTERED, Portal — all check out in
`playthroughs.yaml`. DARK SOULS III and TUNIC were already split in the 2026-07-27 pass; DARK
SOULS: REMASTERED and Portal were never broken — their late 2026 sessions are real replays with
achievement unlocks spread across every day claimed. What's still "wrong" is only `_index.md`'s
`finished:` field, which mirrors whichever session ended last rather than when the game was first
completed — the same **separate pass**, flagged and deferred on 2026-07-27, that this doc still
hasn't touched. `finished:` is genuinely stale (not just late) for **Braid** (`2025-03-23` vs. a
playthrough that ended in 2013), **Danganronpa: Trigger Happy Havoc** (`2022-11-05` vs. 2017), and
**The Witness** (`2024-04-07` vs. 2016) — those three drifted from `playthroughs.yaml` itself
during the last pass, since only `playthroughs.yaml` got rewritten. TUNIC/DS3/DSR/Portal's
`finished:` dates match their (correct) last session exactly, so there's no drift to fix there —
just the open question of whether `finished:` should mean "completed" or "last touched."

## Applied 2026-07-27

**47 of 49 applied** as edited below. Skyrim split into 4 playthroughs and Divinity into 2, per
their notes. Prose notes for Dark Souls III, TUNIC and Evoland went into each game's `_index.md`
body rather than `playthroughs.yaml`, which doesn't preserve comments.

**Still open:**

- **FTL** and **Jackbox 3** — held, not written. Both fail the playtime check (13.5 and
  12.4 h/day against the proposed dates) and need real dates from memory.
- **The Binding of Isaac** — applied as a *floor*, not a truth. 234.7h can't fit in 5 days; the
  months you remember are still unrecorded. Worth revisiting.
- **The Witness** — start date `2016-02-01` is your memory, not archive data. The end
  (`2016-02-07`) is the credits and is solid.
- **`finished:` in `_index.md`** still carries `last_played` dates across many games — Dark Souls
  III reads `2026-04-16`, TUNIC `2026-07-16`, Evoland `2017-01-31`. Untouched; separate pass.

## How to edit this

Each block is the complete session list for that game's playthrough, one session per line.
**Change a date, delete a line, or add a line.** Hand the file back and I'll apply it.

- **Leave a block alone** — accept it as proposed.
- **Delete a whole block** — *defer.* Not "leave it as it is": it means the proposal is wrong and
  you want to come back to it later with information the archive doesn't have. Nothing gets
  applied to that game, and it stays on the list of open questions.

Lines with no `#` comment are sessions you already have, unchanged.

---

### Braid
`braid` · 9.2h · 11/12 · finished

```
2012-11-18..2012-11-18
2012-12-24..2012-12-24
2013-01-20..2013-01-20
```

`Closure` ("Complete the game") unlocked 2013-01-20 15:28:40 — that end date is the credits, not an inference. World 3 solved 13:34, then 4, 5, 6, done by 15:28.

### The Witness
`the-witness` · 22.9h · 1/2

```
2016-02-01..2016-02-07   # was ..2024-04-07
```

**Defer — start date is unsupported.** The end is solid: `Endgame` ("Finished.") at
2016-02-07 19:17:09. But the game has only two achievements total, so nothing records when you
*began*. 22.9h does not fit in one day — this run almost certainly spans days or weeks before the
7th. Needs your memory for a start date.

### The Binding of Isaac
`the-binding-of-isaac` · 234.7h · 84/99

```
2013-12-22..2013-12-22   # was ..2021-05-02
2014-01-08..2014-01-11   # new
```

**Defer — playtime can't fit.** 234.7h across the handful of days below is ~40h/day.
Play clearly continued for months after the unlocks stopped; only 15 of 99 achievements were left
and they're the hard ones, so there was nothing to fire. The archive cannot tell us when you
actually stopped.

### Spyro™ Reignited Trilogy
`spyro-reignited-trilogy` · 12.2h · 28/105

```
2019-10-16..2019-10-19   # was ..2025-08-02
2025-08-02..2025-08-02   # new
```

All 28 unlocks are Spyro 1, across four consecutive days. 12.2h is one game of three — worth a look at whether this should be `dropped`.

### Danganronpa: Trigger Happy Havoc
`danganronpa-trigger-happy-havoc` · 52.9h · 9/38

```
2017-08-06..2017-08-14   # was ..2022-11-05
```

### Into the Breach
`into-the-breach` · 12.8h · 15/70

```
2018-04-12..2018-04-18   # was ..2023-05-21
```

### DARK SOULS™ III
`dark-souls-iii` · 49.1h · 30/43 · finished

```
2018-05-21..2018-05-21   # Tried at roommate's request, bounced off of it after trying Iudex Gundyr
2021-05-01..2021-05-15   # was ..2026-04-16
2026-04-16..2026-04-16   # new
```

`The End of Fire` unlocked 2021-05-15. There's a 7-day gap inside the run (05-02 → 05-09) if you'd rather see it as two sessions.

### AI: The Somnium Files
`ai-the-somnium-files` · 33.2h · 45/45

```
2020-06-29..2020-07-05   # was ..2024-09-20
2024-09-20..2024-09-20   # new
```

### Sekiro™: Shadows Die Twice
`sekiro-shadows-die-twice` · 153.8h · 34/34 · mastered

```
2020-04-21..2020-05-05
2020-06-14..2020-06-14
2020-07-12..2020-07-16   # was ..2024-07-13
2024-07-13..2024-07-13   # new
```

The earlier sessions in this playthrough are already right — Apr 2020
run, June gap-filler, July cleanup. Only the tail is wrong. Worth a sanity check though: 153.8h
across 22 recorded days is 7h/day, on the high side.

### The Talos Principle
`the-talos-principle` · 5.7h · 9/40

```
2019-10-29..2019-11-01   # was ..2023-06-19
2023-06-19..2023-06-19   # new
```

### TUNIC
`tunic` · 49.5h · 36/36

```
2023-01-01..2023-01-28   # was ..2026-07-16
2026-07-16..2026-07-16   # This was part of a multiworld randomizer, wasn't a new playthrough
```

### Super Meat Boy
`super-meat-boy` · 26.6h · 29/48 · finished

```
2012-11-17..2012-11-18
2013-10-07..2013-10-07
2021-01-09..2021-01-15   # was ..2024-04-07
2024-04-07..2024-04-07   # new
```

`The End` unlocked 2021-01-09; 01-10 to 01-15 is mop-up.

### Crash Bandicoot™ N. Sane Trilogy
`crash-bandicoot-n-sane-trilogy` · 22.4h · 48/74

```
2019-08-06..2019-08-14   # was ..2022-05-14
2022-05-14..2022-05-14   # new
```

### Hammerwatch
`hammerwatch` · 20.1h · 6/38 · session-based

```
2013-11-27..2013-11-28
2014-06-23..2014-06-23   # was ..2017-02-24
2017-02-24..2017-02-24   # new
```

### Borderlands 2
`borderlands-2` · 76.7h · 35/75 · finished

```
2015-02-15..2015-02-15
2015-03-19..2015-03-19
2015-06-10..2015-06-21   # was ..2017-12-22
2017-12-22..2017-12-22   # new
```

### Divinity: Original Sin 2
`divinity-original-sin-2` · 31.1h · 25/97

Two separate playthroughs, not multiple attempts at the same playthrough. Dropped both of them

```
2021-02-21..2021-03-01   # was ..2023-08-09
2023-08-05..2023-08-09   # new
```

### Team Fortress 2
`team-fortress-2` · 29.4h · 63/520 · multiplayer

```
2012-07-23..2012-07-27
2012-08-17..2012-09-23
2012-12-08..2012-12-23
2013-02-02..2013-02-02
2013-08-15..2013-08-15
2013-10-08..2013-10-08
2014-01-10..2014-01-10
2014-08-26..2014-08-26   # was ..2017-01-17
2017-01-17..2017-01-17   # new
```

Both unlocks land on one day. The 875-day span was entirely the `last_played` artifact.

### Tabletop Simulator
`tabletop-simulator` · 63.0h · 10/26 · multiplayer

```
2015-06-16..2015-06-16
2016-09-04..2016-09-18
2021-04-12..2021-04-12
2023-09-06..2023-09-06   # was ..2026-01-18
2026-01-18..2026-01-18   # new
```

### Sunless Sea
`sunless-sea` · 11.6h · 5/54

```
2017-01-07..2017-01-12   # was ..2019-05-03
2019-05-03..2019-05-03   # new
```

### The Elder Scrolls V: Skyrim
`the-elder-scrolls-v-skyrim` · 92.2h · 35/75 · dropped

All of these sessions were likely full new playthroughs

```
2014-06-26..2014-06-30
2018-04-20..2018-04-23
2018-05-16..2018-07-05   # was ..2020-08-23
2020-08-23..2020-08-23   # new
```

### Monaco
`monaco` · 11.1h · 2/13 · playing

```
2014-12-18..2014-12-18
2015-09-18..2015-09-18   # was ..2017-12-21
2017-12-21..2017-12-21   # new
```

### FEZ
`fez` · 12.1h · 7/12

```
2013-09-08..2013-09-08   # was ..2015-11-03
2013-09-21..2013-09-21   # new
2015-11-03..2015-11-03   # new
```

### Papers, Please
`papers-please` · 5.0h · 2/13

```
2014-04-12..2014-04-13   # was ..2016-02-27
2016-02-27..2016-02-27   # new
```

### Hollow Knight
`hollow-knight` · 273.1h · 63/63 · mastered

```
2017-03-25..2017-04-30
2017-08-07..2017-08-07
2017-10-06..2017-10-06
2020-12-16..2020-12-19
2022-04-21..2022-05-01
2024-08-11..2024-08-11   # was ..2026-05-02
2026-05-02..2026-05-02   # new
```

This is the last leg of the mastery run — `Embrace the Void` on 2024-08-11 took it to 63/63.

### The Jackbox Party Pack 3
`the-jackbox-party-pack-3` · 37.2h · 4/20 · multiplayer

```
2022-02-01..2022-02-01
2022-09-02..2022-09-02   # was ..2024-04-27
2024-04-27..2024-04-27   # new
```

**Defer — playtime can't fit.** 37.2h across 3 days is 12.4h/day, and
this is a party game played in company. 4/20 achievements means almost nothing was ever recorded.

### FTL: Faster Than Light
`ftl-faster-than-light` · 67.6h · 22/51

```
2021-07-09..2021-07-12   # was ..2023-02-22
2023-02-22..2023-02-22   # new
```

**Defer — playtime can't fit.** 67.6h across 5 days is 13.5h/day. FTL's
remaining achievements are ship-unlock grinds, so a long tail of play would leave no trace.

### Golf With Your Friends
`golf-with-your-friends` · 14.3h · 26/83 · playing

```
2016-08-19..2016-08-19
2020-04-03..2020-05-11
2020-07-05..2020-07-16
2020-12-05..2020-12-05
2021-04-07..2021-04-07   # was ..2022-10-19
2022-10-19..2022-10-19   # new
```

### Hades
`hades` · 73.3h · 31/49 · playing

```
2020-10-23..2020-11-02
2020-12-29..2021-01-14
2021-09-30..2021-10-01
2022-12-19..2022-12-20   # was ..2024-05-26
2024-05-26..2024-05-26   # new
```

### Disco Elysium
`disco-elysium` · 72.8h · 13/45 · finished

```
2020-07-22..2020-08-01
2021-04-29..2021-04-29
2021-05-28..2021-05-28
2021-06-29..2021-06-30   # was ..2022-11-20
2022-11-20..2022-11-20   # new
```

**Low confidence.** Only 2 unlocks in this session across 72.8h total, so the gaps say almost nothing. If you remember playing more of it in late 2021, override this from memory.

### Evoland
`evoland` · 18.0h · 27/29 · finished, speedran

```
2013-11-27..2013-11-27
2016-04-23..2016-05-04   # was ..2017-01-31
2017-01-31..2017-01-31   # new
```

### Red Dead Redemption 2
`red-dead-redemption-2` · 11.6h · 2/51 | dropped

Same playthrough

```
2020-02-02..2020-02-02   # was ..2020-09-19
2020-09-19..2020-09-19   # new
```

### Cookie Clicker
`cookie-clicker` · 596.6h · 485/637 | session-based

```
2021-09-11..2021-11-28   # was ..2022-04-05
2021-12-11..2021-12-12   # new
2021-12-28..2021-12-28   # new
2022-01-08..2022-01-09   # new
2022-01-21..2022-01-30   # new
2022-02-12..2022-02-13   # new
2022-03-06..2022-03-06   # new
2022-04-05..2022-04-05   # new
```

485 unlocks make the gaps readable to the day: one 78-day obsessive stretch, then
six short returns tapering off. Caveat: 596.6h over 98 days is 6h/day — plausible for an idle game
left running, but the sessions may be under-counting.

### Peglin
`peglin` · 24.4h · 10/52 | session-based

```
2022-06-02..2022-06-07   # was ..2022-12-04
2022-12-04..2022-12-04   # new
```

### 20 Minutes Till Dawn
`20-minutes-till-dawn` · 33.0h · 26/34 | session-based

```
2022-06-16..2022-06-19   # was ..2022-11-02
2022-06-30..2022-07-22   # new
2022-11-02..2022-11-02   # new
```

### Bloons TD 6
`bloons-td-6` · 397.4h · 99/156 · session-based

```
2022-07-15..2022-07-15
2022-11-16..2022-12-10
2023-01-15..2023-02-28
2023-12-29..2024-01-05
2024-10-19..2024-10-31
2025-02-12..2025-02-12   # was ..2025-06-15
2025-02-23..2025-02-23   # new
2025-03-08..2025-03-16   # new
2025-03-29..2025-05-25   # new
2025-06-15..2025-06-15   # new
2025-09-01..2025-09-07
2026-05-07..2026-05-07
```

### HITMAN World of Assassination
`hitman-world-of-assassination` · 113.7h · 44/83

```
2023-02-18..2023-03-25   # was ..2023-05-29
2023-04-09..2023-04-09   # new
2023-05-29..2023-05-29   # new
```

### ANIMAL WELL
`animal-well` · 18.1h · 20/20

```
2026-01-24..2026-01-30   # was ..2026-05-03
2026-05-03..2026-05-03   # new
```

`GOOD ENDING` 2026-01-25, then 20/20 by 2026-01-30.

### Monster Train
`monster-train` · 24.6h · 18/53 · playing

```
2020-08-31..2020-09-06
2021-10-10..2021-10-10
2023-02-22..2023-02-22   # was ..2023-05-28
2023-05-28..2023-05-28   # new
```

### Baldur's Gate 3
`baldur-s-gate-3` · 431.0h · 45/54 · finished

```
2023-10-15..2023-11-11
2023-12-02..2023-12-03
2023-12-31..2024-02-11
2024-11-02..2024-11-02
2024-12-01..2024-12-01
2024-12-23..2024-12-25   # was ..2025-03-04
2025-01-14..2025-01-18   # new
2025-01-29..2025-02-04   # new
2025-02-15..2025-03-04   # new
```

Highest-confidence split in the file: four distinct runs at it over the holidays into March, 10–20 day gaps between, 431h total.

### Spelunky 2
`spelunky-2` · 70.8h · 23/32 · playing

```
2020-09-29..2020-10-15
2020-11-13..2020-11-17
2021-01-03..2021-01-03
2026-02-22..2026-02-22   # was ..2026-05-02
2026-03-11..2026-03-11   # new
2026-05-02..2026-05-02   # new
```

### Balatro
`balatro` · 85.7h · 21/31 · playing

```
2024-02-21..2024-04-02
2024-11-30..2024-11-30   # was ..2025-02-05
2025-02-05..2025-02-05   # new
```

### OCTOPATH TRAVELER
`octopath-traveler` · 89.0h · 67/88

```
2022-05-14..2022-06-19   # was ..2022-07-19
2022-07-19..2022-07-19   # new
```

### Paper Mario: The Thousand-Year Door
`paper-mario-the-thousand-year-door` · 123/123

```
2024-10-24..2024-11-09   # was ..2024-12-26
2024-11-20..2024-11-24   # new
2024-12-08..2024-12-14   # new
2024-12-26..2024-12-26   # new
```

RetroAchievements, 123/123. Four clean study-block-shaped stretches.

### Danganronpa 2: Goodbye Despair
`danganronpa-2-goodbye-despair` · 33.5h · 14/47 | finished

```
2017-08-26..2017-09-15   # was ..2017-10-20
2017-10-20..2017-10-20   # new
```

### DARK SOULS™ II: Scholar of the First Sin
`dark-souls-ii-scholar-of-the-first-sin` · 44.2h · 18/38 · finished

```
2020-07-12..2020-07-12
2021-01-04..2021-01-04
2021-03-29..2021-04-04   # was ..2021-05-21
2021-05-21..2021-05-21   # new
```

Real run is the seven days ending on `The Heir`. 2021-05-21 is a relaunch.

### Terraria
`terraria` · 196.6h · 84/137 · session-based

```
2015-08-16..2015-08-16
2017-01-17..2017-02-26
2020-05-16..2020-05-16
2022-01-05..2022-01-06   # was ..2022-02-23
2022-01-23..2022-02-23   # new
```

### Fall Guys
`fall-guys` · 32.4h · 30/34 | multiplayer

```
2020-08-15..2020-09-14   # was ..2020-10-02
2020-09-30..2020-10-02   # new
```

### Super Mega Baseball 2
`super-mega-baseball-2` · 8.7h · 10/42 | multiplayer

```
2020-05-30..2020-06-08   # was ..2020-07-16
2020-06-24..2020-07-16   # new
```

### Outer Wilds
`outer-wilds` · 24.4h · 8/31 · finished

```
2020-08-25..2020-08-28
2021-08-18..2021-08-18   # was ..2021-10-02
2021-09-01..2021-10-02   # new
```

---

## Leaving these alone (4)

Long sessions where the unlocks are spread evenly with no gap over 10 days — genuine sustained
runs, which is what a long session should look like.

Risk of Rain Returns (86d) · Persona 5 Royal (63d) · MultiVersus (56d) · Metaphor: ReFantazio (47d)

## Where the dates came from

The last session of each playthrough took its `finished` date from Steam's `last_played` rather
than from the last achievement unlock. `last_played` updates on *any* launch, including a
one-minute "does this still run" years later — which is how a two-week Dark Souls III run in May
2021 got welded to an April 2026 relaunch and became an 1811-day session.

Start dates were never affected: every session's `started` matches its first unlock exactly.

To rebuild them I took every unlock inside each session from `archive/`, split wherever there was
a **gap of more than 10 days**, and dated each resulting session first-unlock to last-unlock.
Where the old end date was 30+ days past the last unlock, I kept it as a one-day session rather
than deleting it — it does record a launch, just not a play session.

The 10-day threshold is calibrated against splits you made by hand: Elden Ring's
`2022-02-25..2022-03-13` swallows a 5-day gap and `2024-07-05..2024-08-04` swallows a 20-day one.

**This is weakest on games with few achievements.** Disco Elysium has 13 unlocks across 72.8
hours, so its gaps mean very little. Those blocks are flagged inline.

## Follow-up not included here

`finished:` in `_index.md` has the same bug in several places — The Witness is dated
`2024-04-07`, a relaunch, when the credits rolled 2016-02-07. I left front matter untouched.
