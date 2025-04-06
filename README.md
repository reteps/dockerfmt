# dockerfmt

Dockerfile format and parser, and a modern version [dockfmt](https://github.com/jessfraz/dockfmt). Built on top of the internal [buildkit](github.com/moby/buildkit) parser.

## Usage

```bash
dockerfmt version
dockerfmt --check <file1> <file2> ...
dockerfmt <file1> <file2> ...
```

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