# Ongoing-but-quiet triage

Every game that renders as still-open on the timeline (or is `status: playing`) and has not
been touched in 30+ days. As of 2026-07-27. **Edit the first column, then hand it back.**

Actions:

- `close` — fill the trailing open date with the last-played date. Status is unchanged; the game
  just stops drawing an open-ended bar. This is the right call for a party/one-shot game that is
  genuinely session-based, it simply isn't *currently* in progress.
- `leave` — still in rotation, the open bar is honest.
- `dropped` / `finished` / `paused` — the status itself is wrong.

---

## A — open trailing session (25)

These have a `sessions:` list whose last entry has `finished: ""`. Closing it is a one-line edit
and does not touch the status.

| Proposed | Game | Status | Played | Ach | Last played | Quiet |
|---|---|---|---|---|---|---|
| `close` | Rocket League | multiplayer | 11h | 24/88 | 2016-06-29 | 10.1y |
| `close` | Team Fortress 2 | multiplayer | 29h | 63/520 | 2017-01-17 | 9.5y |
| `close` | Hammerwatch | session-based | 20h | 6/38 | 2017-02-24 | 9.4y |
| `close` | Sid Meier's Civilization V | session-based | 40h | 7/286 | 2017-06-20 | 9.1y |
| `close` | Monaco | multiplayer | 11h | 2/13 | 2017-12-21 | 8.6y |
| `close` | 'Cities: Skylines' | session-based | 43h | 17/135 | 2019-06-29 | 7.1y |
| `close` | Spelunky | session-based | 132h | 15/20 | 2020-09-26 | 5.8y |
| `close` | Terraria | session-based | 197h | 84/137 | 2022-02-23 | 4.4y |
| `close` | Golf With Your Friends | multiplayer | 14h | 26/83 | 2022-10-19 | 3.8y |
| `close` | Risk of Rain 2 | session-based | 16h | 25/171 | 2022-10-23 | 3.8y |
| `close` | Enter the Gungeon | session-based | 21h | 7/54 | 2023-02-17 | 3.4y |
| `close` | Monster Train | session-based | 25h | 18/53 | 2023-05-28 | 3.2y |
| `close` | Sid Meier's Civilization VI | session-based | 59h | 37/320 | 2023-06-11 | 3.1y |
| `close` | The Oregon Trail | session-based | 9h | 17/83 | 2023-10-24 | 2.8y |
| `close` | 'The Binding of Isaac: Rebirth' | session-based | 275h | 277/641 | 2023-11-07 | 2.7y |
| `close` | The Jackbox Party Pack 3 | multiplayer | 37h | 4/20 | 2024-04-27 | 2.2y |
| `close` | Hades | session-based | 73h | 31/49 | 2024-05-26 | 2.2y |
| `close` | Dead Cells | session-based | 45h | 41/121 | 2024-06-20 | 2.1y |
| `close` | Oxygen Not Included | session-based | 71h | 10/51 | 2024-10-25 | 1.8y |
| `close` | Balatro | session-based | 86h | 21/31 | 2025-02-05 | 1.5y |
| `close` | Slay the Spire | session-based | 109h | 25/46 | 2025-09-03 | 0.9y |
| `close` | Stardew Valley | session-based | 210h | 28/49 | 2025-12-13 | 0.6y |
| `close` | The Jackbox Party Pack 8 | multiplayer | 10h | 5/20 | 2026-01-10 | 0.5y |
| `close` | Tabletop Simulator | multiplayer | 63h | 10/26 | 2026-01-18 | 0.5y |
| `close` | Spelunky 2 | session-based | 71h | 23/32 | 2026-05-02 | 0.2y |

## B — flat entry, no finished date (20)

Single entry, no sessions, `finished:` empty. Closing means writing a `finished:` date on the
entry — same effect, same one-line edit.

| Proposed | Game | Status | Played | Ach | Last played | Quiet |
|---|---|---|---|---|---|---|
| `close` | A Wizard's Lizard | session-based | 7h | 2/20 | 2016-02-29 | 10.4y |
| `close` | Left 4 Dead 2 | multiplayer | 9h | 6/101 | 2017-04-23 | 9.3y |
| `close` | Sunless Sea | session-based | 12h | 5/54 | 2019-05-03 | 7.2y |
| `close` | Streets of Rogue | session-based | 13h | 26/52 | 2019-08-24 | 6.9y |
| `close` | 60 Parsecs! | session-based | 22h | 12/44 | 2020-01-28 | 6.5y |
| `close` | Super Mega Baseball 2 | multiplayer | 9h | 10/42 | 2020-07-16 | 6.0y |
| `close` | Fall Guys | multiplayer | 32h | 30/34 | 2020-10-02 | 5.8y |
| `close` | The Binding of Isaac | session-based | 235h | 84/99 | 2021-05-02 | 5.2y |
| `close` | Zero Escape: The Nonary Games | paused | 22h | 9/38 | 2022-01-10 | 4.5y |
| `close` | Cookie Clicker | session-based | 597h | 485/637 | 2022-04-05 | 4.3y |
| `close` | MultiVersus | multiplayer | 59h | 26/28 | 2022-09-21 | 3.8y |
| `close` | Dome Keeper | session-based | 11h | 17/47 | 2022-10-11 | 3.8y |
| `close` | 20 Minutes Till Dawn | session-based | 33h | 26/34 | 2022-11-02 | 3.7y |
| `close` | Peglin | session-based | 24h | 10/52 | 2022-12-04 | 3.6y |
| `close` | 'FTL: Faster Than Light' | session-based | 68h | 22/51 | 2023-02-22 | 3.4y |
| `close` | Into the Breach | session-based | 13h | 15/70 | 2023-05-21 | 3.2y |
| `close` | Infinitode 2 | session-based | 8h | 3/45 | 2023-12-23 | 2.6y |
| `close` | Risk of Rain Returns | session-based | 45h | 99/155 | 2024-02-02 | 2.5y |
| `close` | The Great Ace Attorney Chronicles | paused | 42h | 13/30 | 2025-11-13 | 0.7y |
| `close` | Mewgenics | session-based | 54h | 56/281 | 2026-03-19 | 0.4y |

## C — no playthroughs.yaml at all (11)

**Different problem.** These have no logged playthrough *and* no `started:` in front matter, so
they render nowhere on the timeline — 11 invisible rows, not 11 misleading ones. Closing a date
is meaningless here; the fix is to log a playthrough from the archived last-played data, or to
accept that they are archive-only entries. `aseprite` is still the unresolved not-a-game row.

| Proposed | Game | Status | Played | Ach | Last played | Quiet |
|---|---|---|---|---|---|---|
| `???` | Starbound | session-based | 26h | 0/51 | 2014-01-26 | 12.5y |
| `???` | Don't Starve | session-based | 14h | 0/0 | 2014-02-01 | 12.5y |
| `???` | Magicite | session-based | 6h | 0/0 | 2014-06-30 | 12.1y |
| `???` | Realm of the Mad God Exalt | multiplayer | 12h | 0/167 | 2014-11-26 | 11.7y |
| `???` | Z1 Battle Royale | multiplayer | 7h | 0/0 | 2017-03-13 | 9.4y |
| `???` | Among Us | multiplayer | 11h | 0/33 | 2021-03-31 | 5.3y |
| `???` | Golfie | session-based | 9h | 0/21 | 2022-11-02 | 3.7y |
| `???` | TrackMania Nations Forever | multiplayer | 11h | 0/0 | 2022-11-16 | 3.7y |
| `???` | RimWorld | session-based | 241h | 0/0 | 2023-01-10 | 3.5y |
| `???` | Aseprite | playing | 39h | 0/0 | 2026-02-21 | 0.4y |
| `???` | Trackmania | multiplayer | 72h | 0/0 | 2026-05-23 | 0.2y |

