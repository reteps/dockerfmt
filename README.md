# dockerfmt

[![Go](https://img.shields.io/github/go-mod/go-version/reteps/dockerfmt)](https://github.com/reteps/dockerfmt)
[![Release](https://img.shields.io/github/v/release/reteps/dockerfmt)](https://github.com/reteps/dockerfmt/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An opinionated Dockerfile formatter, built on BuildKit's parser, that formats `RUN` commands with [shfmt](https://github.com/mvdan/sh). A successor to [dockfmt](https://github.com/jessfraz/dockfmt).

## Example

Before:

```dockerfile
MAINTAINER me

FROM node:lts-alpine          as       Builder



COPY        .      /app
ENV MY_VAR my-value
ENV a=1 \
  b=2 \
            c=3
RUN apt-get update && \
    #install deps
    apt-get install -y --no-install-recommends \
        vim curl git && \
    rm -rf /var/lib/apt/lists/*
```

After:

```dockerfile
LABEL org.opencontainers.image.authors="me"

FROM node:lts-alpine AS builder

COPY . /app
ENV MY_VAR=my-value
ENV a=1 \
    b=2 \
    c=3
RUN apt-get update \
    # install deps
    && apt-get install -y --no-install-recommends \
        vim curl git \
    && rm -rf /var/lib/apt/lists/*
```

It also fixes the formatting issues that BuildKit's [build checks](https://docs.docker.com/reference/build-checks/) warn about, such as `as` casing, upper-case stage names and `EXPOSE 80/TCP`. Line endings (LF or CRLF) and the trailing newline are kept as they are.

## Install

- **Binaries:** see [releases](https://github.com/reteps/dockerfmt/releases).
- **Go:** `go install github.com/reteps/dockerfmt@latest`
- **npm:** `@reteps/dockerfmt` ([docs](js/README.md))
- **Docker:**

  ```bash
  docker run --rm -v "$PWD:/pwd" ghcr.io/reteps/dockerfmt /pwd/Dockerfile
  # with --write, run as yourself so the file stays yours
  docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/pwd" ghcr.io/reteps/dockerfmt --write /pwd/Dockerfile
  ```

  The default image has no shell. For CI systems that need one, such as GitLab CI, use `ghcr.io/reteps/dockerfmt:alpine`.

## Usage

```bash
dockerfmt Dockerfile        # print the formatted file
dockerfmt -w Dockerfile     # format in place
dockerfmt -c Dockerfile     # exit 1 if not formatted
dockerfmt -d Dockerfile     # show what would change
cat Dockerfile | dockerfmt  # read stdin
```

```
  -c, --check             Check if the file(s) are formatted
  -d, --diff              Print a unified diff of the changes needed (implies --check)
  -i, --indent uint       Number of spaces to use for indentation (default 4)
  -n, --newline           End the file with a trailing newline (--newline=false: without one)
  -s, --space-redirects   Put a space after redirect operators (--space-redirects=false: none) (default true)
  -t, --use-tabs          Indent with tabs instead of spaces
  -w, --write             Write the formatted output back to the file(s)
```

### EditorConfig

When formatting files (not stdin), dockerfmt reads `.editorconfig`. Command-line flags win.

| Property               | Flag                |
| ---------------------- | ------------------- |
| `indent_style = tab`   | `--use-tabs`        |
| `indent_size`          | `--indent`          |
| `insert_final_newline` | `--newline`         |
| `space_redirects`      | `--space-redirects` |

### Pre-commit

```yaml
repos:
  - repo: https://github.com/reteps/dockerfmt
    rev: v1.0.0
    hooks:
      - id: dockerfmt
```

The hook runs `dockerfmt --write --newline`. Setting `args` replaces those flags.

### Skipping an instruction

Put `# dockerfmt-ignore` on the line before it:

```dockerfile
# dockerfmt-ignore
RUN   echo   "left exactly as written"
```

## Safety

dockerfmt checks its own work. Each formatted instruction is parsed again with BuildKit and compared with the original. If the meaning differs, or formatting it again would change it, the instruction is left as written and a warning like this is printed:

```
Dockerfile:12: left unformatted: output differs in meaning
```

Please [report](https://github.com/reteps/dockerfmt/issues) these. The only intended changes in behavior are the fixes BuildKit recommends: `MAINTAINER` becomes a `LABEL`, and `FROM Builder` refers to the stage `builder`.

## Limitations

These are left as written:

- commands for a non-POSIX shell, such as PowerShell or `cmd` (set by `SHELL` or a Windows base image)
- heredocs that aren't shell scripts, such as `RUN python3 <<EOF`
- shell commands with backquotes, or in files with a non-default `# escape=`
- long JSON-form commands, which are not wrapped

Input must be UTF-8.

## Contributing

Issues and pull requests are welcome. The expected output lives in [`tests/golden`](tests/golden), one [txtar](https://pkg.go.dev/golang.org/x/tools/txtar) file per area: add your input under `cases` and the output under `want`.
