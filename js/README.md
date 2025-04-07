# `@reteps/dockerfmt`

Bindings around the Golang `dockerfmt` tooling.


```js
import { formatDockerfile } from '@reteps/dockerfmt'
const result = await formatDockerfile('../tests/comment.dockerfile', { indent: 4, trailingNewline: true })

console.log(result)
```
