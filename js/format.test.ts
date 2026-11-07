import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { formatDockerfile, formatDockerfileContents } from './node.js'

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

    it('keeps CRLF line endings', async () => {
        const result = await formatDockerfileContents(
            'from alpine\r\n# comment\r\nrun   echo hi\r\n',
            defaultOptions,
        )
        assert.equal(result, 'FROM alpine\r\n# comment\r\nRUN echo hi\r\n')
    })

    it('rejects a parse error', async () => {
        await assert.rejects(
            formatDockerfileContents(
                'FROM alpine\nRUN <<EOF\n',
                defaultOptions,
            ),
            { message: 'line 2: unterminated heredoc' },
        )
        // The WASM module keeps working after an error.
        const result = await formatDockerfileContents(
            'from alpine',
            defaultOptions,
        )
        assert.equal(result, 'FROM alpine\n')
    })

    it('rejects UTF-16 decoded as UTF-8', async () => {
        // What Buffer.toString() makes of a UTF-16 LE file without a BOM.
        const input = Buffer.from('FROM alpine\n', 'utf16le').toString()
        await assert.rejects(formatDockerfileContents(input, defaultOptions), {
            message: /^line 1: NUL byte found/,
        })
    })
})

describe('formatDockerfile', () => {
    const withFile = async (
        contents: Buffer,
        fn: (fileName: string) => Promise<void>,
    ) => {
        const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'dockerfmt-'))
        try {
            const fileName = path.join(dir, 'Dockerfile')
            await fs.writeFile(fileName, contents)
            await fn(fileName)
        } finally {
            await fs.rm(dir, { recursive: true })
        }
    }

    it('formats a file and keeps its byte order mark', async () => {
        await withFile(Buffer.from('﻿from alpine\n'), async (fileName) => {
            const result = await formatDockerfile(fileName, defaultOptions)
            assert.equal(result, '﻿FROM alpine\n')
        })
    })

    it('rejects a UTF-16 file (issue #81)', async () => {
        const utf16 = Buffer.concat([
            Buffer.from([0xff, 0xfe]),
            Buffer.from('FROM alpine\nRUN echo hi\n', 'utf16le'),
        ])
        await withFile(utf16, async (fileName) => {
            await assert.rejects(formatDockerfile(fileName, defaultOptions), {
                message: `${fileName}: file is encoded as UTF-16; re-save it as UTF-8`,
            })
        })
    })

    it('rejects invalid UTF-8', async () => {
        await withFile(
            Buffer.from('FROM alpine\n# caf\xe9\n', 'latin1'),
            async (fileName) => {
                await assert.rejects(
                    formatDockerfile(fileName, defaultOptions),
                    {
                        message: `${fileName}: invalid UTF-8; re-save it as UTF-8`,
                    },
                )
            },
        )
    })
})
