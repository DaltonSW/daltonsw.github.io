---
title: Introducing headline 📰
showDate: false
date: 2026-07-08
url: /headline
customCSS: gamejams.css
# TODO: Make this not game-jam-specific
---
{{< card >}}

## Welcome!

Generate banner + social-preview PNG images from a single JSON config.

`headline` renders a large title, a subtitle, a row of tag pills, and a repo URL as HTML/CSS and rasterizes them to PNG via [Ogre](https://github.com/macawls/ogre) - no terminal, no screenshots. The title is a real vector font at a large point size (with optional drop shadow and gradient) and the supporting text is sized independently, so everything stays crisp at any scale.

{{</ card >}}

{{< card >}}

## Installation

### Go 🖥️

- Have `Go`
- Have your `Go` install location on your `$PATH`
- Run the following:
```sh
go install go.dalton.dog/headline@latest
```

### From source

```sh
go build -o headline .
```

Fonts are embedded, so the binary works with no external files.

{{</ card >}}

{{< card >}}

## Usage

```
headline                                                Render every variant (banner + social)
headline banner  [--config path.json] [-o out.png]      Render the wide banner
headline social  [--config path.json] [-o out.png]      Render the social preview
```

| Flag              | Meaning                                                        |
|-------------------|------------------------------------------------------------------|
| `--config`        | Path to a JSON config. Omit to use `./headline.json` if present, else the built-in defaults. |
| `-o`, `--output`  | Output PNG path. Defaults to `<out_dir>/<name>.png`. Can't be combined with `--count`. |
| `--seed`          | Seed for generated colors/fonts/tint, overriding `title.seed` for this run. Mutually exclusive with `--reroll`. |
| `--reroll`        | Draw a fresh random seed from `crypto/rand`, print it, and render with it. Nothing is written back to the config. |
| `--count`         | Render this many variants, each with its own seed, so you can pick a favorite. |

```sh
headline banner --config headline.json
headline social --config headline.json

headline --reroll                    # re-roll generated colors/fonts/tint (prints the seed)
headline --seed a3f9c1               # reproduce a specific rerolled look
headline --count 20                  # render 20 variants to pick a favorite from
```

Generated colors, gradient, font pairing, and background tint are all seeded from `content.title` (or `title.seed`, if set), so re-rendering the same config always reproduces the same look. Only fields left blank in the config are generated; anything you pin explicitly is never touched by `--seed`/`--reroll`/`--count`.

{{</ card >}}

{{< card >}}

## Configuration

Pass a config with `--config`; any omitted field falls back to the default. The config has six sections:

- **`content`** - the words: `title`, `subtitle`, `tags`, `repo_url`
- **`title`** - the large vector title: font, size, color/gradient, letter spacing, line spacing, drop shadow. Leave colors and gradient direction blank and they're generated deterministically from a seed
- **`body`** - shared font/size/color for the subtitle, tag pills, and repo URL
- **`border`** - optional frame around the canvas, solid or gradient, inheriting the title's colors by default
- **`background`** - canvas tint blended in from the title's color
- **`output`** - image sizes and output directory for `banner` and `social`

Set `title.font` / `body.font` to a Google Fonts family name (e.g. `"Bebas Neue"`) or a local `.ttf`/`.otf` path. Leave both blank and headline picks a curated, seeded (title, body) Google Fonts pairing for you.

{{</ card >}}
