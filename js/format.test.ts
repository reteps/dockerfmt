import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { formatDockerfile, formatDockerfileContents } from './node.js'
import { formatDockerfileContents as browserFormat } from './format.js'

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

    it('keeps the trailing newline by default', async () => {
        assert.equal(await formatDockerfileContents('from a\n', {}), 'FROM a\n')
        assert.equal(await formatDockerfileContents('from a', {}), 'FROM a')
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

    it('applies defaults for omitted options', async () => {
        const input = 'from alpine\nrun echo a \\\n  && echo b'
        const expected = 'FROM alpine\nRUN echo a \\\n    && echo b'
        assert.equal(await formatDockerfileContents(input), expected)
        assert.equal(await formatDockerfileContents(input, {}), expected)
    })

    it('rejects invalid options', async () => {
        const cases: [unknown, ErrorConstructor, RegExp][] = [
            [null, TypeError, /options must be an object/],
            ['4', TypeError, /options must be an object/],
            [{ indent: '2' }, TypeError, /indent must be an integer/],
            [{ indent: 2.5 }, TypeError, /indent must be an integer/],
            [{ indent: NaN }, TypeError, /indent must be an integer/],
            [{ indent: 0 }, RangeError, /indent must be from 1 to 16/],
            [{ indent: -1 }, RangeError, /indent must be from 1 to 16/],
            [{ indent: 17 }, RangeError, /indent must be from 1 to 16/],
            [{ trailingNewline: 1 }, TypeError, /trailingNewline must be a/],
            [{ spaceRedirects: 'no' }, TypeError, /spaceRedirects must be a/],
            [{ onFallback: true }, TypeError, /onFallback must be a function/],
        ]
        for (const [options, name, message] of cases) {
            await assert.rejects(
                formatDockerfileContents('FROM alpine', options as any),
                (err: Error) =>
                    err.constructor === name && message.test(err.message),
                `options ${JSON.stringify(options)}`,
            )
        }
        await assert.rejects(formatDockerfileContents(42 as any), TypeError)
        assert.equal(
            await formatDockerfileContents('from alpine', defaultOptions),
            'FROM alpine\n',
        )
    })

    it('formats concurrent calls', async () => {
        const inputs = Array.from({ length: 20 }, (_, i) => `from alpine:${i}`)
        const results = await Promise.all(
            inputs.map((input, i) =>
                formatDockerfileContents(input, {
                    ...defaultOptions,
                    indent: (i % 16) + 1,
                }),
            ),
        )
        assert.deepEqual(
            results,
            inputs.map((_, i) => `FROM alpine:${i}\n`),
        )
    })

    // Mostly "\r\n" line endings, but formatting the file with them would add
    // a "\r" to each line of the heredoc script, so it is left as written.
    const fallbackInput =
        'FROM alpine\r\nRUN  echo a\r\nRUN  echo b\r\nRUN <<EOF\necho x\nEOF\n'

    it('instantiates the WASM module once', async () => {
        let loads = 0
        const getWasm = () => {
            loads++
            return fs.readFile(new URL('format.wasm', import.meta.url))
        }
        await Promise.all([
            browserFormat('from a', defaultOptions, getWasm),
            browserFormat('from b', defaultOptions, getWasm),
            formatDockerfileContents('from c', defaultOptions),
        ])
        await browserFormat('from d', defaultOptions, getWasm)
        assert.ok(loads <= 1, `loaded ${loads} times`)
    })

    it('reports fallbacks to onFallback', async () => {
        const fallbacks: [number, string][] = []
        const result = await formatDockerfileContents(fallbackInput, {
            ...defaultOptions,
            onFallback: (line, reason) => fallbacks.push([line, reason]),
        })
        assert.equal(result, fallbackInput)
        assert.equal(fallbacks.length, 1)
        assert.equal(fallbacks[0][0], 4)
        assert.match(fallbacks[0][1], /line endings/)
    })

    it('rejects with the error thrown by onFallback', async () => {
        const thrown = new Error('stop')
        await assert.rejects(
            formatDockerfileContents(fallbackInput, {
                onFallback: () => {
                    throw thrown
                },
            }),
            (err) => err === thrown,
        )
        assert.equal(
            await formatDockerfileContents('from alpine', defaultOptions),
            'FROM alpine\n',
        )
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
