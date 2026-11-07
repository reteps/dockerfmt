# `@reteps/dockerfmt`

Bindings around the Golang `dockerfmt` tooling. It compiles the Go code to WebAssembly (using standard Go's `GOOS=js GOARCH=wasm` target), which is then used in the JS bindings.

```js
import { formatDockerfile } from '@reteps/dockerfmt'
// Alternatively, you can use `formatDockerfileContents` to format a string instead of a file.

const result = await formatDockerfile('../tests/comment.dockerfile', { indent: 4, trailingNewline: true })

console.log(result)
```

Both functions reject with an `Error` if the input can't be formatted: it isn't valid UTF-8 (for example a UTF-16 file), or it isn't a valid Dockerfile.

## CLI

The package also ships the `dockerfmt` CLI.

```sh
# Format a Dockerfile and print to stdout
npx @reteps/dockerfmt Dockerfile
```
