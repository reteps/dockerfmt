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
- [Configuration](#configuration)
- [Ignoring Directives](#ignoring-directives)
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
- Keeps the file's line endings: CRLF files stay CRLF (a file with mixed line endings gets its most common one)
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

The default image is minimal: it contains only the `dockerfmt` binary and no shell. An Alpine-based variant, which also has a shell, is published as `ghcr.io/reteps/dockerfmt:alpine` (and `:<version>-alpine`). Use it for CI systems that run job scripts inside the image, such as GitLab CI:

```yaml
dockerfmt:
  image:
    name: ghcr.io/reteps/dockerfmt:alpine
    entrypoint: [""]
  script:
    - dockerfmt --check Dockerfile
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

# same, but print a unified diff of the changes needed
dockerfmt -d Dockerfile
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
  -d, --diff              Print a unified diff of the changes needed (implies --check)
  -h, --help              help for dockerfmt
  -i, --indent uint       Number of spaces to use for indentation (default 4)
  -n, --newline           End the file with a trailing newline
  -s, --space-redirects   Redirect operators will be followed by a space
  -w, --write             Write the formatted output back to the file(s)
```

## Configuration

### EditorConfig

dockerfmt reads [EditorConfig](https://editorconfig.org/) files to pick up project-level formatting defaults. The following properties are supported:

| EditorConfig property    | dockerfmt equivalent | Notes                       |
| ------------------------ | -------------------- | --------------------------- |
| `indent_size`            | `--indent`           | Standard EditorConfig key   |
| `insert_final_newline`   | `--newline`          | Standard EditorConfig key   |
| `space_redirects`        | `--space-redirects`  | Custom key (non-standard)   |

`.editorconfig` files are looked up from each Dockerfile's own directory upward (stopping at `root = true`), as the EditorConfig spec describes, so a repository-root `.editorconfig` also applies to Dockerfiles in subdirectories such as `docker/Dockerfile`.

CLI flags always take precedence over EditorConfig values (this includes flags set in a pre-commit hook's `args`; see [Pre-commit](#pre-commit)).

Example `.editorconfig`:

```ini
[Dockerfile]
indent_size = 2
insert_final_newline = true
```

> **Note:** EditorConfig is only applied when formatting files by path.
> It is not used when reading from stdin, since there is no file path to resolve against.

## Safety

dockerfmt never knowingly changes what a Dockerfile does. After formatting each instruction, it parses the result with BuildKit (the parser `docker build` uses) and compares it with the original: ENV/LABEL/ARG values as BuildKit expands them, COPY/ADD sources and destinations, flags, exec-form arrays, heredoc contents, and shell commands (ignoring layout). If anything differs, or the formatter fails, that instruction is left exactly as written and a warning is printed to stderr:

```
Dockerfile:12: left unformatted: output differs in meaning
```

Please [report](https://github.com/reteps/dockerfmt/issues) these warnings; they point at formatter bugs.

Some content is deliberately left as written:

- `RUN`/`CMD`/`ENTRYPOINT` bodies after a `SHELL` instruction that selects a non-POSIX shell (PowerShell, `cmd`), until the next `FROM`
- heredocs fed to another program (`RUN python3 <<EOF`, `RUN <<EOF python3`, or a body with a non-shell shebang), and all `COPY`/`ADD` heredocs
- shell bodies that the shell printer would split into several statements, such as `a; b`, `{ ...; }` groups, or multi-line `if`/`for`
- instructions dockerfmt doesn't know

## Ignoring Directives

To skip formatting for a specific directive, place a `# dockerfmt-ignore` comment on the line immediately before it:

```dockerfile
# dockerfmt-ignore
RUN   echo   "this stays exactly as-is"
RUN echo "this gets formatted normally"
```

The ignore comment applies only to the next directive. This is useful as an escape hatch for output you don't like:

```dockerfile
# dockerfmt-ignore
RUN { echo hello && echo world; }

# dockerfmt-ignore
RUN echo hello; echo world
```

## Pre-commit

Add to your `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/reteps/dockerfmt
    rev: main  # run `pre-commit autoupdate` to pin a version
    hooks:
      - id: dockerfmt
```

The hook runs `dockerfmt --write --newline --space-redirects` by default. Setting `args` replaces those defaults, and like any CLI flag, flags passed in `args` take precedence over `.editorconfig`. To let `.editorconfig` control a setting (e.g. `indent_size`), leave the corresponding flag out of `args`.

## Limitations

- Shell bodies with several statements (`a; b`, `{ ...; }` groups) are left unformatted; see [Safety](#safety).
- With a `# escape=` parser directive other than backslash, shell-form `RUN`/`CMD`/`ENTRYPOINT` bodies are left unformatted.
- No line wrapping for long JSON-form commands.
- Input must be UTF-8 (a UTF-8 byte order mark is kept). Other encodings, such as the UTF-16 that Windows PowerShell's `>` writes by default, are rejected with an error, as are files BuildKit can't parse; dockerfmt then exits non-zero and leaves the file unchanged.

Contributions welcome — please file issues for bugs or feature requests.
