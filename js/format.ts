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
    const go = new Go() // Defined in wasm_exec.js

    const wasmBuffer = await getWasm()
    const wasm = await WebAssembly.instantiate(wasmBuffer, go.importObject)

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
    ) => string

    if (typeof formatBytes !== 'function') {
        throw new Error('dockerfmt WASM module did not register formatBytes')
    }

    return formatBytes(
        fileContents,
        options.indent,
        options.trailingNewline,
        options.spaceRedirects,
    )
}

export const formatDockerfile = () => {
    throw new Error(
        '`formatDockerfile` is not implemented in the browser. Use `formatDockerfileContents` instead.',
    )
}
