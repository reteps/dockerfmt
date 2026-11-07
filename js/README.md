# `@reteps/dockerfmt`

Bindings around the Golang `dockerfmt` tooling. It compiles the Go code to WebAssembly (using standard Go's `GOOS=js GOARCH=wasm` target), which is then used in the JS bindings.

```js
import { formatDockerfile } from '@reteps/dockerfmt'
// Alternatively, you can use `formatDockerfileContents` to format a string instead of a file.

const result = await formatDockerfile('../tests/comment.dockerfile', {
    indent: 4,
    trailingNewline: true,
})

console.log(result)
```

## Options

All options are optional.

| Option            | Type                                     | Default | Description                                                                                                         |
| ----------------- | ---------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------- |
| `indent`          | integer from 1 to 16                     | `4`     | Spaces to indent continuation lines by                                                                              |
| `trailingNewline` | boolean                                  | `false` | End the output with a newline                                                                                       |
| `spaceRedirects`  | boolean                                  | `false` | Put a space after shell redirect operators (`> file`)                                                               |
| `onFallback`      | `(line: number, reason: string) => void` |         | Called for each instruction left as written because formatting it would have changed its meaning, or failed (a bug) |

An option of the wrong type rejects with a `TypeError`, and an `indent` out of range with a `RangeError`. An error thrown by `onFallback` stops formatting and is passed through as the rejection.

Both functions reject with an `Error` if the input can't be formatted: it isn't valid UTF-8 (for example a UTF-16 file), or it isn't a valid Dockerfile.

The WebAssembly module is instantiated on the first call and reused by later ones.

## Browser

In the browser, `formatDockerfileContents` takes a third argument that loads the WebAssembly module (exported as `@reteps/dockerfmt/format.wasm`; serve it however your bundler handles assets) as a `BufferSource` such as an `ArrayBuffer` or `Uint8Array`:

```js
import { formatDockerfileContents } from '@reteps/dockerfmt'

const getWasm = async () => (await fetch('/format.wasm')).arrayBuffer()
const result = await formatDockerfileContents('from alpine', {}, getWasm)
```

## CLI

The package also ships the `dockerfmt` CLI.

```sh
# Format a Dockerfile and print to stdout
npx @reteps/dockerfmt Dockerfile
```
