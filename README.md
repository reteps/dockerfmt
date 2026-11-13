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

FROM node:lts-alpine AS builder

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
- Converts legacy `ENV` and `LABEL` syntax (`ENV key value` → `ENV key=value`)
- Upgrades deprecated directives (`MAINTAINER` → `LABEL`)
- Fixes what BuildKit's [build checks](https://docs.docker.com/reference/build-checks/) flag: `AS` in the case of `FROM` (FromAsCasing), lower-case stage names and references to them (StageNameCasing), lower-case `EXPOSE` protocols (ExposeProtoCasing), no blank lines inside continuations (NoEmptyContinuation), and the two above (LegacyKeyValueFormat, MaintainerDeprecated)
- Puts a space after the `#` of comments (`#comment` → `# comment`), leaving parser directives, shebangs and `#####` banners alone
- Supports heredocs in `RUN` steps
- Keeps the file's line endings: CRLF files stay CRLF (a file with mixed line endings gets its most common one, unless that would change a heredoc or quoted value)
- Keeps the trailing newline as it is, unless told otherwise
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
# print the formatted file
docker run --rm -v "$PWD:/pwd" ghcr.io/reteps/dockerfmt:latest /pwd/Dockerfile

# format in place, as your user so the rewritten file stays yours
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/pwd" ghcr.io/reteps/dockerfmt:latest --write /pwd/Dockerfile
```

The image runs as root by default. With `-w`/`--write`, pass `-u "$(id -u):$(id -g)"` so dockerfmt runs as your own user: anything it writes is then owned by you rather than root, and it can only change files you could change yourself.

The default image is minimal: it is based on [`gcr.io/distroless/static-debian12`](https://github.com/GoogleContainerTools/distroless) and contains only the `dockerfmt` binary, CA certificates and timezone data, with no shell. An Alpine-based variant, which also has a shell, is published as `ghcr.io/reteps/dockerfmt:alpine` (and `:<version>-alpine`). Use it for CI systems that run job scripts inside the image, such as GitLab CI:

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

# read from stdin (also as "-" among other files)
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
  -n, --newline           End the file with a trailing newline (--newline=false: without one)
  -s, --space-redirects   Put a space after redirect operators (--space-redirects=false: none) (default true)
  -t, --use-tabs          Indent with tabs instead of spaces
  -w, --write             Write the formatted output back to the file(s)
```

The output ends with a newline if the input does. `--newline` adds one, and `--newline=false` removes it; `insert_final_newline` in `.editorconfig` does the same.

`--indent` must be between 1 and 16. `--write` can't be combined with `--check` or `--diff`, or used with stdin. It rewrites each file in place, so the file keeps its permissions, owner and hard links; while writing, a backup of the original is kept next to it and restored if the write fails.

`--check` prints `<file> is not formatted` and `--diff` prints the diff, both to stdout; errors and warnings go to stderr. Exit status is 1 if any input couldn't be read, parsed or written, or, with `--check` or `--diff`, isn't formatted; otherwise 0.

A file named `version` must be given as `./version`, since `dockerfmt version` prints the version.

## Configuration

### EditorConfig

dockerfmt reads [EditorConfig](https://editorconfig.org/) files to pick up project-level formatting defaults. The following properties are supported:

| EditorConfig property    | dockerfmt equivalent | Notes                       |
| ------------------------ | -------------------- | --------------------------- |
| `indent_style`           | `--use-tabs`         | Standard EditorConfig key   |
| `indent_size`            | `--indent`           | Standard EditorConfig key   |
| `insert_final_newline`   | `--newline`          | Standard EditorConfig key   |
| `space_redirects`        | `--space-redirects`  | Custom key (non-standard)   |

Property names are case-insensitive. `indent_size = tab` uses `tab_width`, and indentation must be from 1 to 16. An invalid value is reported as a warning and ignored; some, such as an invalid `insert_final_newline`, make the EditorConfig library reject the whole file, which is then ignored with a warning.

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

Apart from the build-check fixes above, dockerfmt never knowingly changes what a Dockerfile does. After formatting each instruction, it parses the result with BuildKit (the parser `docker build` uses) and compares it with the original: ENV/LABEL/ARG values as BuildKit expands them, COPY/ADD sources and destinations, flags, exec-form arrays, heredoc contents, and shell commands (ignoring layout). Formatting the result again must also leave it unchanged. If anything differs, or the formatter fails, that instruction is left exactly as written and a warning is printed to stderr:

```
Dockerfile:12: left unformatted: output differs in meaning
```

Finally, the whole formatted file is parsed again, as written with its line endings, and compared instruction by instruction with the input. This catches what checking one instruction at a time can't, such as a file with mixed line endings where converting them would change a heredoc. If that check fails, the file is left unchanged.

Please [report](https://github.com/reteps/dockerfmt/issues) these warnings; they point at formatter bugs, except for files with mixed line endings or stray carriage returns.

The formatter is fuzz tested for these properties (`go test ./lib -fuzz FuzzFormat`).

Some content is deliberately left as written:

- `RUN`/`CMD`/`ENTRYPOINT` bodies after a `SHELL` instruction that selects a non-POSIX shell (PowerShell, `cmd`), until the next `FROM`
- heredocs fed to another program (`RUN python3 <<EOF`, `RUN <<EOF python3`, or a body with a non-shell shebang), and all `COPY`/`ADD` heredocs
- instructions dockerfmt doesn't know

## Ignoring Directives

To skip formatting for a specific directive, make `# dockerfmt-ignore` the last comment before it (blank lines may come between):

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

The hook runs `dockerfmt --write --newline` by default. Setting `args` replaces those defaults, and like any CLI flag, flags passed in `args` take precedence over `.editorconfig`. To let `.editorconfig` control a setting (e.g. `indent_size`), leave the corresponding flag out of `args`.

## Limitations

- With a `# escape=` parser directive other than backslash, shell-form `RUN`/`CMD`/`ENTRYPOINT` bodies are left unformatted.
- Shell bodies with backquoted command substitutions are left unformatted, since rewriting them as `$(...)` can change their meaning.
- A stage's shell is worked out from `SHELL` instructions, from the stage it is built on, and from well-known Windows base images (which default to `cmd`); a stage built on another Windows image, named through a build argument for example, is formatted as POSIX shell. Add a `SHELL` instruction or `# dockerfmt-ignore` there.
- No line wrapping for long JSON-form commands.
- Input must be UTF-8 (a UTF-8 byte order mark is kept). Other encodings, such as the UTF-16 that Windows PowerShell's `>` writes by default, are rejected with an error, as are files BuildKit can't parse; dockerfmt then exits non-zero and leaves the file unchanged.

Contributions welcome — please file issues for bugs or feature requests.

Formatting is tested with golden files in [`tests/golden`](tests/golden), one [txtar](https://pkg.go.dev/golang.org/x/tools/txtar) archive per area: each input is followed by `want`, what it must format to. Each one is also checked for idempotency, with CRLF line endings and without a final newline, and for instructions left unformatted. To add a case, append it to the area's `cases` input and `want` under a `# <description>` comment.
