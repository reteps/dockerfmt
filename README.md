# dockerfmt

Dockerfile format and parser, and a modern version of [dockfmt](https://github.com/jessfraz/dockfmt). Built on top of the internal [buildkit](github.com/moby/buildkit) parser.

## Installation

Binaries are available from the [releases](https://github.com/reteps/dockerfmt/releases) page.

## Usage

```output
A updated version of the dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh.

Usage:
  dockerfmt [Dockerfile] [flags]
  dockerfmt [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  version     Print the version number of dockerfmt

Flags:
  -c, --check         Check if the file(s) are formatted
  -h, --help          help for dockerfmt
  -i, --indent uint   Number of spaces to use for indentation (default 4)
  -n, --newline       End the file with a trailing newline
  -w, --write         Write the formatted output back to the file(s)

Use "dockerfmt [command] --help" for more information about a command.
```

## Limitations

- The `RUN` parser currently doesn't support grouping or semicolons in commands
- No line wrapping is performed for long JSON commands
- The `# escape=X` directive is not supported

Contributions are welcome!

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