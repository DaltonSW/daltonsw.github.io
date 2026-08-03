---
title: "{{ replace .File.ContentBaseName "-" " " | title }}"
platform: ""
status: "playing"   # playing | finished | dropped | paused — no "backlog" for a logged run
started: {{ now.Format "2006-01-02" }}
finished:
rating:
draft: false
---

Notes on this particular playthrough. The game association is automatic (inherited via
the parent game's `cascade` block) — no `games:` field needed here.
