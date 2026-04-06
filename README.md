# dockerfmt

[![Go](https://img.shields.io/github/go-mod/go-version/reteps/dockerfmt)](https://github.com/reteps/dockerfmt)
[![Release](https://img.shields.io/github/v/release/reteps/dockerfmt)](https://github.com/reteps/dockerfmt/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An opinionated Dockerfile formatter. Formats directives, normalizes whitespace, and uses [shfmt](https://github.com/mvdan/sh) to format shell commands inside `RUN` steps.

Spiritual successor to [dockfmt](https://github.com/jessfraz/dockfmt), built on the [buildkit](https://github.com/moby/buildkit) parser.

## Table of Contents

- [Demo](#demo)
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
- [Pre-commit](#pre-commit)
- [Limitations](#limitations)

## Demo

```dockerfile
MAINTAINER me

FROM node:lts-alpine          as       builder



COPY        .      /app
WORKDIR         /app
ENV MY_VAR my-value
ENV a=1 \
  b=2 \
            c=3
RUN apt-get update && \
    # install deps
    apt-get install -y --no-install-recommends \
        vim curl git && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete
```

```dockerfile
LABEL org.opencontainers.image.authors="me"

FROM node:lts-alpine as builder

COPY . /app
WORKDIR /app
ENV MY_VAR=my-value
ENV a=1 \
    b=2 \
    c=3
RUN apt-get update \
    # install deps
    && apt-get install -y --no-install-recommends \
        vim curl git \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && find /tmp -not -path /tmp -delete
```

## Features

- Formats shell commands in `RUN` steps via [shfmt](https://github.com/mvdan/sh) — consistent `&&` chains, indentation, quoting
- Preserves and re-aligns inline comments, even across multiline `RUN` steps
- Normalizes whitespace and extra blank lines across all directives
- Converts legacy `ENV` syntax (`ENV key value` → `ENV key=value`)
- Upgrades deprecated directives (`MAINTAINER` → `LABEL`)
- Supports heredocs in `RUN` steps
- Reads from files or stdin
- Pre-commit hook support
- JS/WASM bindings for Node.js ([docs](js/README.md))
- Docker image for CI use

## Installation

### Binaries

Download from the [releases](https://github.com/reteps/dockerfmt/releases) page.

### go install

```bash
go install github.com/reteps/dockerfmt@latest
```

### Docker

```bash
docker run --rm -v $(pwd):/pwd ghcr.io/reteps/dockerfmt:latest /pwd/Dockerfile
```

## Usage

```bash
# format and print to stdout
dockerfmt Dockerfile

# format in place
dockerfmt -w Dockerfile

# read from stdin
cat Dockerfile | dockerfmt

# check if already formatted (exits non-zero if not)
dockerfmt -c Dockerfile
```

```
Usage:
  dockerfmt [Dockerfile...] [flags]
  dockerfmt [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  version     Print the version number of dockerfmt

Flags:
  -c, --check             Check if the file(s) are formatted
  -h, --help              help for dockerfmt
  -i, --indent uint       Number of spaces to use for indentation (default 4)
  -n, --newline           End the file with a trailing newline
  -s, --space-redirects   Redirect operators will be followed by a space
  -w, --write             Write the formatted output back to the file(s)
```

## Pre-commit

Add to your `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/reteps/dockerfmt
    rev: main  # run `pre-commit autoupdate` to pin a version
    hooks:
      - id: dockerfmt
        args:
          - --indent=4
          - --newline
          - --write
```

## Limitations

- The `RUN` formatter does not support command grouping (`{ \`) or unescaped semicolons — these are returned unformatted.
- The `# escape=X` parser directive is not supported.
- No line wrapping for long JSON-form commands.

Contributions welcome — please file issues for bugs or feature requests.
