---
title: "{{ replace .File.ContentBaseName "-" " " | title }}"
platform: ""
status: "playing"   # overall status: backlog | playing | finished | dropped
started: {{ now.Format "2006-01-02" }}
finished:
rating:
cover:
draft: false
cascade:
  params:
    games: ["{{ .File.ContentBaseName }}"]
playthroughs: []  # entries: started, finished, status (playing|finished|dropped|paused),
                  # rating, notes, sessions ([{started,finished}], optional)
---

A sentence or two on the game overall. This is the implicit first playthrough — only add a
`playthroughs:` entry for a replay, or a full nested write-up page for something that deserves
its own URL.
