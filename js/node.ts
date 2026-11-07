import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
    formatDockerfileContents as formatDockerfileContents_,
    FormatOptions,
} from './format.js'

const getWasm = () => {
    return fs.readFile(
        path.resolve(
            path.dirname(fileURLToPath(import.meta.url)),
            'format.wasm',
        ),
    )
}

export const formatDockerfileContents = async (
    fileContents: string,
    options?: FormatOptions,
) => {
    return formatDockerfileContents_(fileContents, options, getWasm)
}

// Unlike buffer.toString(), this rejects invalid UTF-8 instead of replacing it
// with U+FFFD (which would corrupt the file if the result is written back),
// and keeps a byte order mark so the output keeps it too.
const decodeUtf8 = (fileName: string, buffer: Uint8Array) => {
    try {
        return new TextDecoder('utf-8', {
            fatal: true,
            ignoreBOM: true,
        }).decode(buffer)
    } catch {
        const utf16 =
            (buffer[0] === 0xff && buffer[1] === 0xfe) ||
            (buffer[0] === 0xfe && buffer[1] === 0xff)
        throw new Error(
            `${fileName}: ${utf16 ? 'file is encoded as UTF-16' : 'invalid UTF-8'}; re-save it as UTF-8`,
        )
    }
}

export const formatDockerfile = async (
    fileName: string,
    options?: FormatOptions,
) => {
    const fileBuffer = await fs.readFile(fileName)
    const fileContents = decodeUtf8(fileName, fileBuffer)
    return formatDockerfileContents(fileContents, options)
}

export { FormatOptions }
