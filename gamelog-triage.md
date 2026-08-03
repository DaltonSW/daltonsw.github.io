# GameLog status triage

86 games still marked `playing`, triaged against archived Steam/RetroAchievements data
(as of 2026-07-27). Playtime is compared against the game's typical length; completion is
achievements unlocked.

## How to edit this

Change the **Proposed** cell to whatever you want applied, then hand the file back.
Leave a row alone to accept my call. Delete a row to leave that game as `playing`.

Valid values: `dropped` `finished` `mastered` `paused` `playing` `session-based` `multiplayer`

Bucket C rows are `???` on purpose - I'm not guessing those.

> Caveat on bucket B: I judged these by comparing your playtime against typical game length
> from my own knowledge, not from a data source. Solid for the clear cases (Portal at 11h on a
> 3h game); genuinely uncertain for the mid-range ones.

---

## Already applied (Step 1, 11 games)

Reclassified to `session-based`: Civ V, Civ VI, Terraria, Starbound, Stardew Valley, Magicite,
Golfie, 60 Parsecs, The Oregon Trail. To `multiplayer`: Monaco, Super Mega Baseball 2.

---

## Not in question

**Stays `playing`** (4, still warm): Lorelei and the Laser Eyes (13d), Professor Layton and the Last Specter (5d), Professor Layton and the Last Specter [Subset - Mouse Alley] (9d), Lies of P (1d)

| Proposed | Game | Played | Achievements | Quiet | Why |
|---|---|---|---|---|---|
| `mastered` | TUNIC | 49h | 36/36 | 11d | 36/36 achievements, finished 11d ago - a real mastery, not a drop |

---

## A - Confident drop (11)

Playtime far short of the game's length, low completion, years quiet. I'd apply these without hesitation.

| Proposed | Game | Played | Achievements | Quiet | Why |
|---|---|---|---|---|---|
| `dropped` | Papers, Please | 5h | 2/13 | 10yr | 5h and 2/13 - the ending achievements never fired |
| `dropped` | The Witcher 3: Wild Hunt | 6h | 1/78 | 8yr | 6h against a ~50h main story, 1/78. Barely left White Orchard |
| `dropped` | Turmoil | 6h | 10/39 | 8yr | 6h, 10/39, quiet since 2018 |
| `dropped` | XCOM 2 | 10h | 19/88 | 7yr | 10h against a ~40h campaign, 19/88 |
| `dropped` | Borderlands: The Pre-Sequel | 17h | 13/63 | 6yr | 17h against a ~30h campaign, 13/63 |
| `dropped` | Halo: The Master Chief Collection | 7h | 18/700 | 6yr | 7h across six campaigns, 18/700 |
| `dropped` | Red Dead Redemption 2 | 11h | 2/51 | 5yr | 11h against a ~50h story, 2/51. Abandoned in chapter 1-2 |
| `dropped` | Bug Fables: The Everlasting Sapling | 6h | 4/30 | 4yr | 6h against a ~30h RPG, 4/30 |
| `dropped` | LEGO® Star Wars™: The Skywalker Saga | 5h | 5/45 | 3yr | 5h against a ~20h story, 5/45 |
| `dropped` | The Talos Principle | 5h | 9/40 | 3yr | 5h against a ~20h game, 9/40 |
| `dropped` | Divinity: Original Sin 2 | 31h | 25/97 | 2yr | 31h against a ~80h campaign, 25/97. Stalled around act 2 |


## B - Almost certainly finished, not dropped (56)

Playtime at or past the game's length with solid completion. This is where `gamelog stale` would go wrong: its rule marks anything under 90% completion as `dropped`, which would mislabel every one of these.

| Proposed | Game | Played | Achievements | Quiet | Why |
|---|---|---|---|---|---|
| `finished` | SteamWorld Dig | 5h | 13/24 | 12yr | 5h on a ~5h game, 13/24 |
| `finished` | FEZ | 12h | 7/12 | 10yr | Speedrun - the 12h is practice, not progress. Speedrunning it means you beat it |
| `finished` | Life is Strange™ | 16h | 18/60 | 10yr | 16h across five episodes (~13h), 18/60 |
| `finished` | Undertale | 22h | none | 10yr | 22h and no Steam achievements exist; enough for multiple routes |
| `finished` | Portal 2 | 27h | 34/51 | 10yr | 27h, 34/51 - campaign plus co-op |
| `finished` | Evoland | 17h | 27/29 | 9yr | 17h on a ~4h game, 27/29 |
| `finished` | Ori and the Blind Forest: Definitive Edition | 24h | 49/57 | 9yr | 24h on a ~10h game, 49/57 |
| `finished` | Danganronpa V3: Killing Harmony | 39h | 18/41 | 8yr | 39h on a ~35h VN, 18/41 |
| `finished` | Danganronpa 2: Goodbye Despair | 33h | 14/47 | 8yr | 33h on a ~30h VN, 14/47 |
| `finished` | Borderlands 2 | 76h | 35/75 | 8yr | 76h, 35/75 |
| `finished` | Hacknet | 7h | 10/11 | 8yr | 7h on a ~8h game, 10/11 |
| `finished` | Fallout 4 | 53h | 29/84 | 8yr | 53h, 29/84 |
| `finished` | A Hat in Time | 33h | 30/46 | 7yr | 33h on a ~10h game, 30/46 |
| `finished` | Assassin's Creed Odyssey | 91h | 63/93 | 6yr | 91h, 63/93 |
| `dropped` | The Elder Scrolls V: Skyrim | 92h | 35/75 | 5yr | 92h, 35/75 |
| `finished` | BioShock Infinite | 13h | 30/80 | 5yr | 13h on a ~12h game, 30/80 |
| `finished` | Going Under | 12h | 17/23 | 5yr | 12h on a ~10h game, 17/23 |
| `finished` | Divinity: Original Sin Enhanced Edition | 67h | 35/54 | 5yr | 67h, 35/54 |
| `finished` | Tangle Tower | 6h | 9/10 | 5yr | 6h on a ~6h game, 9/10 |
| `finished` | Dishonored | 159h | 26/80 | 5yr | Speedrun - the 159h is practice, not progress. Speedrunning it means you beat it |
| `finished` | DARK SOULS™ II: Scholar of the First Sin | 44h | 18/38 | 5yr | 44h on a ~40h game, 18/38 |
| `finished` | DEATHLOOP | 23h | 24/58 | 4yr | 23h on a ~20h game, 24/58 |
| `finished` | Beyond: Two Souls | 14h | 24/45 | 4yr | 14h on a ~10h game, 24/45 |
| `finished` | Crash Bandicoot™ N. Sane Trilogy | 22h | 48/74 | 4yr | 22h, 48/74 across the three games |
| `finished` | OCTOPATH TRAVELER | 89h | 67/88 | 4yr | 89h, 67/88 |
| `finished` | The Hex | 7h | 10/28 | 3yr | 7h on a ~4h game, 10/28 |
| `finished` | Cult of the Lamb | 23h | 37/57 | 3yr | 23h on a ~15h game, 37/57 |
| `finished` | Death's Door | 10h | 14/24 | 3yr | 10h on a ~10h game, 14/24 |
| `finished` | Persona 4 Golden | 136h | 46/50 | 3yr | 136h, 46/50 |
| `finished` | Cursed to Golf | 6h | 8/13 | 3yr | 6h on a ~8h game, 8/13 |
| `finished` | Danganronpa: Trigger Happy Havoc | 52h | 9/38 | 3yr | 52h on a ~25h VN, 9/38 |
| `finished` | Fallout: New Vegas | 68h | 26/75 | 3yr | 68h, 26/75 |
| `finished` | Card Shark | 9h | 33/52 | 3yr | 9h on a ~8h game, 33/52 |
| `finished` | Disco Elysium | 72h | 13/45 | 3yr | 72h on a ~30h game, 13/45 |
| `finished` | STAR WARS Jedi: Fallen Order™  | 20h | 20/39 | 3yr | 20h on a ~18h game, 20/39 |
| `finished` | South Park™: The Stick of Truth™ | 14h | 22/50 | 3yr | 14h on a ~14h game, 22/50 |
| `finished` | Neon White | 52h | 58/63 | 3yr | 52h, 58/63 |
| `dropped` | DREDGE | 12h | 27/60 | 3yr | 12h on a ~12h game, 27/60 |
| `finished` | HITMAN World of Assassination | 113h | 44/83 | 3yr | 113h, 44/83 - endless by design but thoroughly played |
| `finished` | Super Meat Boy | 26h | 29/48 | 2yr | 26h, 29/48 |
| `finished` | The Witness | 22h | 1/2 | 2yr | 22h on a ~20h game (only 2 achievements exist, so % is meaningless) |
| `finished` | Grand Theft Auto V Legacy | 55h | 20/77 | 2yr | 55h against a ~30h story, 20/77 |
| `finished` | Celeste | 51h | 30/32 | 1yr | 51h, 30/32 |
| `finished` | Inscryption | 19h | 23/40 | 1yr | 19h on a ~14h game, 23/40 |
| `finished` | Baldur's Gate 3 | 431h | 45/54 | 1yr | 431h, 45/54 |
| `finished` | The Roottrees are Dead | 20h | 14/22 | 1yr | 20h on a ~10h game, 14/22 |
| `finished` | Braid | 9h | 11/12 | 1yr | 9h on a ~5h game, 11/12 |
| `dropped` | DAVE THE DIVER | 28h | 24/43 | 1yr | 28h on a ~30h game, 24/43 |
| `finished` | TY the Tasmanian Tiger | 15h | 37/39 | 1yr | 15h, 37/39 |
| `finished` | Metaphor: ReFantazio | 138h | 33/44 | 333d | 138h on a ~80h RPG, 33/44 |
| `paused` | The Great Ace Attorney Chronicles | 42h | 13/30 | 256d | 42h across both games, 13/30 |
| `finished` | Portal | 11h | 12/15 | 198d | 11h on a ~3h game, 12/15 |
| `dropped` | Nine Sols | 35h | 28/35 | 155d | 35h on a ~30h game, 28/35 |
| `finished` | DARK SOULS™ III | 49h | 30/43 | 102d | 49h on a ~35h game, 30/43 |
| `finished` | Strange Antiquities | 7h | 18/22 | 99d | 7h, 18/22 |
| `finished` | DARK SOULS™: REMASTERED | 113h | 22/41 | 90d | 113h, 22/41 |


## C - Needs your memory (14)

Signal is void, contradictory, or the game is a multi-part collection.

| Proposed | Game | Played | Achievements | Quiet | Why |
|---|---|---|---|---|---|
| `finished` | Hyper Light Drifter | 10h | 6/23 | 8yr | 10h on a ~9h game but only 6/23 - right on the line |
| `dropped` | XCOM: Enemy Unknown | 17h | 37/85 | 7yr | 17h against a ~30h campaign, 37/85. Finished on easy or abandoned? |
| `dropped` | Assassin's Creed Brotherhood | 16h | none | 7yr | 16h on a ~15h story, but no Steam achievements exist to confirm |
| `dropped` | LEGO® Harry Potter: Years 1-4 | 18h | none | 6yr | 18h on a ~15h game, but no Steam achievements exist to confirm |
| `finished` | Paradise Killer | 15h | 0/39 | 5yr | 15h on a ~17h game. Achievements predate your playthrough |
| `finished` | Outer Wilds | 24h | 8/31 | 4yr | 24h on a ~20h game, but 8/31 - did you reach the ending or stall? |
| `paused` | Zero Escape: The Nonary Games | 21h | 9/38 | 4yr | 21h, 9/38. Enough for 999 but not both games |
| `dropped` | EarthBound | 0h | 14/105 | 3yr | RetroAchievements, 14/105, no playtime signal at all |
| `finished` | Far Cry 5 | 42h | 0/72 | 2yr | 42h. Achievements predate your playthrough, so 0/72 means nothing |
| `finished` | Far Cry 6 | 62h | 0/99 | 2yr | 62h. Achievements predate your playthrough, so 0/99 means nothing |
| `finished` | Shovel Knight: Treasure Trove | 38h | 32/138 | 1yr | 38h, 32/138. Which of the four campaigns did you finish? |
| `dropped` | Spyro™ Reignited Trilogy | 12h | 28/105 | 359d | 12h, 28/105. Roughly one of three games finished |
| `???` | Aseprite | 38h | none | 156d | Not a game - it is a pixel-art editor. Belongs in content/games at all? |
| `dropped` | Mario & Luigi: Bowser's Inside Story | 0h | 41/109 | 130d | RetroAchievements, 41/109, no playtime signal; played 4 months ago |


