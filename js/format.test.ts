import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { formatDockerfileContents } from './node.js'

const defaultOptions = {
    indent: 4,
    trailingNewline: true,
    spaceRedirects: false,
}

describe('formatDockerfileContents', () => {
    it('formats a basic Dockerfile', async () => {
        const input = `from alpine
run echo hello
`.trim()

        const result = await formatDockerfileContents(input, defaultOptions)
        assert.equal(result, 'FROM alpine\nRUN echo hello\n')
    })

    it('formats CMD JSON form with spaces', async () => {
        const input = `FROM alpine
CMD ["ls","-la"]
`.trim()

        const result = await formatDockerfileContents(input, defaultOptions)
        assert.equal(result, 'FROM alpine\nCMD ["ls", "-la"]\n')
    })

    it('formats RUN JSON form with spaces', async () => {
        const input = `FROM alpine
RUN ["echo","hello"]
`.trim()

        const result = await formatDockerfileContents(input, defaultOptions)
        assert.equal(result, 'FROM alpine\nRUN ["echo", "hello"]\n')
    })

    it('handles the issue #25 reproduction case', async () => {
        const input = `
FROM nginx
WORKDIR /app
ARG PROJECT_DIR=/
ARG NGINX_CONF=nginx.conf
COPY $NGINX_CONF /etc/nginx/conf.d/nginx.conf
COPY $PROJECT_DIR /app
CMD mkdir --parents /var/log/nginx && nginx -g "daemon off;"
`.trim()

        const result = await formatDockerfileContents(input, {
            indent: 4,
            spaceRedirects: false,
            trailingNewline: true,
        })

        assert.ok(result.includes('FROM nginx'))
        assert.ok(result.includes('WORKDIR /app'))
        assert.ok(result.endsWith('\n'))
    })

    it('respects trailingNewline: false', async () => {
        const input = 'FROM alpine'
        const result = await formatDockerfileContents(input, {
            ...defaultOptions,
            trailingNewline: false,
        })
        assert.ok(!result.endsWith('\n'))
    })

    it('respects indent option', async () => {
        const input = `FROM alpine
RUN echo a \\
  && echo b
`.trim()

        const result = await formatDockerfileContents(input, {
            ...defaultOptions,
            indent: 2,
        })
        assert.ok(result.includes('  && echo b'))
    })
})
