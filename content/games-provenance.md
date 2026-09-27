---
title: "Games provenance"
build:
  render: never
  list: never
---

Each provider tracks achievements, playtime, and dates differently. Some hand over
everything automatically, others hand over almost nothing. Here's what's pulled straight
from each provider, and what had to be filled in by hand.

### RetroAchievements

- Achievement unlocks, dates, points, and icons all come from the API. The most complete and reliable source in the library.
- **Mastered** and **completed** status come from RetroAchievements directly, not a guess.
- Matching multiple playthroughs against one combined achievement history is done by hand.

### Steam

- Achievement unlock state, timestamps, names, descriptions, icons, and playtime (total and per-device) all come from the API.
- Steam doesn't track when you played, only totals. Date ranges are estimated from achievement timestamps, or entered by hand for games with none.
- Steam has no "finished" flag. Completion status is a judgment call, not API data.

### PSN

- Trophy unlock state, timestamps, tier (bronze/silver/gold/platinum), and hidden-trophy status all come from the API, joined with names/descriptions/icons from a separate call.
- Earning platinum is treated as a completion signal.
- A few very old PSN games were logged before this API access existed and still have hand-written notes instead of structured trophy data.

### Ubisoft

- Ubisoft has no public achievement API. This data is mirrored through a third-party tracking site: unlock timestamps, icons, achievement count, completion percentage, and playtime.
- Locked achievements aren't tracked at all. Only what's been unlocked shows up.
- Achievement names are usually a raw identifier, not a real title, since the mirror doesn't carry Ubisoft's actual text.

### Xbox

- Xbox 360 has no public achievement API either. This data comes from an unofficial third-party service: unlocked achievements, timestamps, gamerscore, total achievement count, and last-played date.
- Locked achievements aren't returned by this source, only unlocked ones.
- Some of the oldest unlocks are missing a reliable date and show as unlocked but undated.

### Trackmania

- Trackmania has no achievements. Instead, every official campaign's tracks come from Nadeo's own services: personal-best times, medals, and when each time was set.
- Both the best time set while a campaign was live and the all-time best are kept, so going back later to improve a medal shows up as its own thing.

### Bloons TD 6

- The BTD6 save comes from Ninja Kiwi's own data API: rank, tower and hero progress, map completion across difficulties and modes, and more. Its Steam achievements are counted separately, through Steam.
- The save is a snapshot rather than a history, so it shows where things stand as of the last fetch, not when each thing happened.

---

- If a game was played across more than one platform or session, deciding which one gets credit for a provider's achievement history is always a human call. Providers only track achievements per game, not per individual playthrough.
- Refreshing a provider's data never removes anything already captured. Unlocks stay unlocked, and a failed fetch just leaves existing history untouched.
- Session dates for a playthrough are logged by hand. Games played through more than once have been split into separate playthroughs where the break between runs was clear. A gap in achievement unlocks alone doesn't prove a new run started, so a few may still be one long entry.
