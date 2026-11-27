---
title: "{{ replace .File.ContentBaseName "-" " " | title }}"
platform: ""
retroachievements_id:  # numeric ID from the game's retroachievements.org URL, optional
steam_appid:           # numeric appid from the game's Steam store URL, optional
# Also optional: psn_id, ubisoft_id, xbox_id, nadeo_account_id, ninjakiwi_user_id —
# easiest to set from the provider-IDs form in `gamelog serve`.
status: "playing"   # backlog | playing | finished | dropped | unfinished | tried | paused | endless | software — "mastered" is derived from achievements, not set here
tags: []            # free-form, e.g. [multiplayer] — what kind of game it is, separate from status
rating:
cover:
draft: false
cascade:
  params:
    games: ["{{ .File.ContentBaseName }}"]
---

A sentence or two on the game overall. The game itself carries no dates: started, first finished
and last played are all derived from its playthroughs (log one for every run, including the
first), and mastered from its achievements.

Playthroughs live in `playthroughs.yaml` beside this file, maintained by `tools/gamelog`.
Captured provider history lives in `archive/<provider>/<id>.json` at the repo root, keyed by the
IDs above so it survives this entry being renamed or deleted. New games are normally created from
`gamelog serve` rather than this archetype.
