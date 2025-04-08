# `@reteps/dockerfmt`

Bindings around the Golang `dockerfmt` tooling. It uses [tinygo](https://github.com/tinygo-org/tinygo) to compile the Go code to WebAssembly, which is then used in the JS bindings.


```js
import { formatDockerfile } from '@reteps/dockerfmt'
// Alternatively, you can use `formatDockerfileContents` to format a string instead of a file.

const result = await formatDockerfile('../tests/comment.dockerfile', { indent: 4, trailingNewline: true })

console.log(result)
```
