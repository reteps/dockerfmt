import './wasm_exec.js'

export interface FormatOptions {
    /**
     * Spaces to indent continuation lines by, from 1 to 16, or 0 for tabs.
     * Default: 4.
     */
    indent?: number
    /** Indent with tabs instead of spaces. Default: false. */
    useTabs?: boolean
    /**
     * End the output with a newline (true) or without one (false). Default:
     * as the input does.
     */
    trailingNewline?: boolean
    /** Put a space after shell redirect operators (`> file`). Default: false. */
    spaceRedirects?: boolean
    /**
     * Called for each instruction left as written because formatting it would
     * have changed its meaning, or failed. `line` counts from 1.
     */
    onFallback?: (line: number, reason: string) => void
}

type ResolvedOptions = Required<Omit<FormatOptions, 'onFallback'>> &
    Pick<FormatOptions, 'onFallback'>

type FormatBytes = (
    contents: string,
    options: ResolvedOptions,
) => { output: string } | { error: unknown }

const describe = (value: unknown) =>
    typeof value === 'string' ? JSON.stringify(value) : String(value)

const resolveBoolean = (
    options: FormatOptions,
    key: 'trailingNewline' | 'spaceRedirects' | 'useTabs',
    fallback: boolean,
) => {
    const value = options[key] ?? fallback
    if (typeof value !== 'boolean') {
        throw new TypeError(
            `dockerfmt: options.${key} must be a boolean, got ${describe(value)}`,
        )
    }
    return value
}

const resolveOptions = (
    options: FormatOptions | undefined = {},
    fileContents: string,
): ResolvedOptions => {
    if (typeof options !== 'object' || options === null) {
        throw new TypeError(
            `dockerfmt: options must be an object, got ${describe(options)}`,
        )
    }
    const { indent = 4, onFallback } = options
    if (!Number.isInteger(indent)) {
        throw new TypeError(
            `dockerfmt: options.indent must be an integer, got ${describe(indent)}`,
        )
    }
    if (indent < 0 || indent > 16) {
        throw new RangeError(
            `dockerfmt: options.indent must be from 0 (tabs) to 16, got ${indent}`,
        )
    }
    if (onFallback !== undefined && typeof onFallback !== 'function') {
        throw new TypeError(
            `dockerfmt: options.onFallback must be a function, got ${describe(onFallback)}`,
        )
    }
    return {
        indent,
        trailingNewline: resolveBoolean(
            options,
            'trailingNewline',
            fileContents.endsWith('\n'),
        ),
        spaceRedirects: resolveBoolean(options, 'spaceRedirects', false),
        // shfmt, and so prettier-plugin-sh, use indent 0 for tabs.
        useTabs: resolveBoolean(options, 'useTabs', false) || indent === 0,
        onFallback,
    }
}

const instantiate = async (
    getWasm: () => Promise<BufferSource>,
    onExit: () => void,
) => {
    // Our namespaced Go class, not globalThis.Go, which other Go WASM
    // packages may own (see the wasm_exec.js modifications).
    const GoClass = (globalThis as any).__dockerfmt_Go as typeof Go
    const go = new GoClass()
    const { instance } = await WebAssembly.instantiate(
        await getWasm(),
        go.importObject,
    )

    // run() settles only if Go's main() exits, which it shouldn't.
    go.run(instance).then(onExit, onExit)

    // main() registers formatBytes before it blocks, and run() returns once
    // it blocks.
    const global = globalThis as any
    const formatBytes = global.__dockerfmt_formatBytes as FormatBytes
    delete global.__dockerfmt_formatBytes
    if (typeof formatBytes !== 'function') {
        throw new Error('dockerfmt: WASM module did not register formatBytes')
    }
    return formatBytes
}

// One Go runtime serves every call. Calls into it are synchronous, so
// concurrent formatDockerfileContents calls can't interleave inside it. It is
// dropped if starting it fails or it exits, so the next call starts afresh.
let runtime: Promise<FormatBytes> | undefined

const getRuntime = (getWasm: () => Promise<BufferSource>) => {
    if (runtime === undefined) {
        const forget = () => {
            if (runtime === started) {
                runtime = undefined
            }
        }
        const started = instantiate(getWasm, forget)
        started.catch(forget)
        runtime = started
    }
    return runtime
}

export const formatDockerfileContents = async (
    fileContents: string,
    options: FormatOptions | undefined,
    getWasm: () => Promise<BufferSource>,
): Promise<string> => {
    if (typeof fileContents !== 'string') {
        throw new TypeError(
            `dockerfmt: fileContents must be a string, got ${typeof fileContents}`,
        )
    }
    const resolved = resolveOptions(options, fileContents)
    if (typeof getWasm !== 'function') {
        throw new TypeError('dockerfmt: getWasm must be a function')
    }
    const formatBytes = await getRuntime(getWasm)

    // wasm_exec.js decodes strings returned by Go with a TextDecoder that
    // drops a leading byte order mark, so keep it out of the WASM round trip.
    const bom = fileContents.startsWith('﻿') ? '﻿' : ''

    const result = formatBytes(fileContents.slice(bom.length), resolved)
    if ('error' in result) {
        throw result.error
    }
    return bom + result.output
}

export const formatDockerfile = () => {
    throw new Error(
        '`formatDockerfile` is not implemented in the browser. Use `formatDockerfileContents` instead.',
    )
}
