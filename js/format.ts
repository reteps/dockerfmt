import './wasm_exec.js'

export interface FormatOptions {
    indent: number
    trailingNewline: boolean
    spaceRedirects: boolean
}

export const formatDockerfileContents = async (
    fileContents: string,
    options: FormatOptions,
    getWasm: () => Promise<Buffer>,
) => {
    // Use our namespaced Go class instead of globalThis.Go to avoid conflicts
    // with other Go WASM packages (see wasm_exec.js modifications).
    const GoClass = (globalThis as any).__dockerfmt_Go as typeof Go
    const go = new GoClass()

    const wasmBuffer = await getWasm()
    const wasm = await WebAssembly.instantiate(
        wasmBuffer as BufferSource,
        go.importObject,
    )

    /**
     * Do not await this promise, because it only resolves once the go main()
     * function has exited. But we need the main function to stay alive to be
     * able to call the formatBytes function.
     */
    go.run(wasm.instance)

    const formatBytes = (globalThis as any).__dockerfmt_formatBytes as (
        contents: string,
        indent: number,
        trailingNewline: boolean,
        spaceRedirects: boolean,
    ) => string | Error

    if (typeof formatBytes !== 'function') {
        throw new Error('dockerfmt WASM module did not register formatBytes')
    }

    // wasm_exec.js decodes strings returned by Go with a TextDecoder that
    // drops a leading byte order mark, so keep it out of the WASM round trip.
    const bom = fileContents.startsWith('﻿') ? '﻿' : ''

    // The WASM side returns an Error, rather than throwing, when the input
    // can't be formatted (not UTF-8, or a Dockerfile parse error).
    const result = formatBytes(
        fileContents.slice(bom.length),
        options.indent,
        options.trailingNewline,
        options.spaceRedirects,
    )
    if (typeof result !== 'string') {
        throw result
    }
    return bom + result
}

export const formatDockerfile = () => {
    throw new Error(
        '`formatDockerfile` is not implemented in the browser. Use `formatDockerfileContents` instead.',
    )
}
