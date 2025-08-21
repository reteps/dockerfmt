# dockerfmt

Dockerfile formatter, and a modern version of [dockfmt](https://github.com/jessfraz/dockfmt). Built on top of the internal [buildkit](https://github.com/moby/buildkit) parser.

## Installation

### Binaries

Binaries are available from the [releases](https://github.com/reteps/dockerfmt/releases) page.

### go install

```bash
go install github.com/reteps/dockerfmt@latest
```

### docker

```bash
docker run --rm -v $(pwd):/pwd ghcr.io/reteps/dockerfmt:latest /pwd/tests/in/run2.dockerfile
```

## Usage

```output
A updated version of the dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh.

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

Use "dockerfmt [command] --help" for more information about a command.
```

## Pre-commit

You can add the following entry to your `.pre-commit-config.yaml` file to use
`dockerfmt` as a pre-commit hook:

```yaml
repos:
  - repo: https://github.com/reteps/dockerfmt
    # run `pre-commit autoupdate` to pin the version
    rev: main
    hooks:
      - id: dockerfmt
        args:
          # optional: add additional arguments here
          - --indent=4
          - --write
```

## Limitations

- The `RUN` parser currently doesn't support grouping or semicolons in commands. Adding semicolon support is a non-trivial task.

- No line wrapping is performed for long JSON commands
- The `# escape=X` directive is not supported

Contributions are welcome!

## Issues

- This is not production software until the `1.0.0` release, please treat it as such.
- Please file issues for any bugs or feature requests!

## Features

- Format `RUN` steps with <https://github.com/mvdan/sh>
- Support for basic heredocs:

  ```dockerfile
  RUN <<EOF
  echo "hello"
  echo "world"
  EOF
  ```

- Support for basic inline comments in run steps:

  ```dockerfile
  RUN echo "hello" \
      # this is a comment
      && echo "world"
  ```

  ```dockerfile
  RUN echo "hello" \
      # this is a comment
      # that spans multiple lines
      && echo "world"
  ```

  This is surprisingly [non-trivial](https://github.com/moby/buildkit/issues/5889) as we want to attach the comments to their position in the formatted output, but they are stripped by the parser beforehand.

## JS Bindings

The JS bindings are available in the `js` directory. More information on how to use them can be found in the [README](js/README.md) file.
