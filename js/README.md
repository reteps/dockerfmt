# `@reteps/dockerfmt`

Bindings around the Golang `dockerfmt` tooling. It compiles the Go code to WebAssembly (using standard Go's `GOOS=js GOARCH=wasm` target), which is then used in the JS bindings.


```js
import { formatDockerfile } from '@reteps/dockerfmt'
// Alternatively, you can use `formatDockerfileContents` to format a string instead of a file.

const result = await formatDockerfile('../tests/comment.dockerfile', { indent: 4, trailingNewline: true })

console.log(result)
```

## CLI

The package also ships the `dockerfmt` CLI, so you can use it via `npx` or as a
dev dependency in JS/CI workflows without separately installing the Go binary:

```sh
# Format a Dockerfile and print to stdout
npx dockerfmt Dockerfile

# Read from stdin
echo 'from alpine' | npx dockerfmt

# Format in place
npx dockerfmt -w Dockerfile

# Check mode (exits non-zero if any file is not formatted)
npx dockerfmt -c Dockerfile
```

This is **the same binary** as the standalone Go tool — npm just ships the
prebuilt binary for your platform (as an optional dependency) and a small
launcher that execs it. Flags, defaults, EditorConfig support and exit codes are
therefore identical to the Go CLI; see the [main README](../README.md) for the
full reference.

Prebuilt binaries are published for `linux-x64`, `linux-arm64`, `darwin-x64`,
`darwin-arm64`, and `win32-x64`. On other platforms, install the Go binary from
the [releases page](https://github.com/reteps/dockerfmt/releases) instead.
