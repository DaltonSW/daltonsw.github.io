---
title: prism
tagline: "Make your unit testing a bit easier on the eyes."
dev_status: stable
category: main
date: 2025-09-27
repo: https://github.com/DaltonSW/prism
weight: 60
draft: false
---
{{< card >}}

## Welcome!

`prism` makes your unit testing a bit easier on the eyes. Anywhere you'd run `go test`, use
`prism` instead.

{{</ card >}}

{{< card >}}

## Installation

### GitHub Releases

- Go to the `Releases` tab of the repo [here](https://github.com/DaltonSW/prism/releases)
- Download the latest archive for your OS/architecture
- Extract it and place the resulting binary on your `$PATH` and ensure it is executable
```sh
cd ~/Downloads # Assuming you downloaded it here
tar -xvf prism_[whatever].tar.gz # x: Extract; v: Verbose output; f: Specify filename
chmod +x prism # Make file executable
mv prism [somewhere on your $PATH] # Move the file to somewhere on your path for easy execution
```

### Homebrew

Ensure you have `brew` [installed](https://brew.sh). Then, run the following:
```sh
brew install --cask daltonsw/tap/prism
```

### Go

Ensure you have `Go` [installed](https://go.dev/doc/install), and your `Go` install location on
your `$PATH`. Then, run the following:
```sh
go install go.dalton.dog/prism@latest
```

{{</ card >}}

{{< card >}}

## Usage

Just run `prism` in your module directory. Anywhere you'd run `go test`, use `prism` instead.
That's it!

{{</ card >}}
