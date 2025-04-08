import './wasm_exec.js';

interface FormatOptions {
    indent: number;
    trailingNewline: boolean;
}

const isNode: boolean = typeof process !== "undefined" && process.versions != null && process.versions.node != null;

const formatDockerfile = async (fileName: string, options: FormatOptions) => {
    if (!isNode) {
        throw new Error('formatDockerfile is only supported in Node.js');
    }
    const fs = require('node:fs/promises');

    // This would only work in Node.js, so we don't add a wasmDownload function
    const fileBuffer = await fs.readFile(fileName);
    const fileContents = fileBuffer.toString();
    return formatDockerfileContents(fileContents, options);
}

const getWasmModule = () => {
    if (isNode) {
        const path = require('node:path');
        const url = require('node:url');
        const fs = require('node:fs/promises');
        return fs.readFile(path.resolve(path.dirname(url.fileURLToPath(import.meta.url)), 'format.wasm'));
    }
    // In the browser, we need to fetch the wasm module
    throw new Error('WASM module not found. Please provide a function to fetch the WASM module.');
}

const formatDockerfileContents = async (fileContents: string, options: FormatOptions, getWasm: () => Promise<Buffer> = getWasmModule) => {
    const go = new Go()  // Defined in wasm_exec.js
    const encoder = new TextEncoder()
    const decoder = new TextDecoder()

    // get current working directory
    const wasmBuffer = await getWasm();
    const wasm = await WebAssembly.instantiate(wasmBuffer, go.importObject);
    
    /**
     * Do not await this promise, because it only resolves once the go main()
     * function has exited. But we need the main function to stay alive to be
     * able to call the `parse` and `print` function.
    */
    go.run(wasm.instance)
   
    const { memory, malloc, free, formatBytes } = wasm.instance.exports as {
        memory: WebAssembly.Memory
        malloc: (size: number) => number
        free: (pointer: number) => void
        formatBytes: (
            pointer: number,
            length: number,
            indent: number,
            trailingNewline: boolean
        ) => number
      }



    const fileBufferBytes = encoder.encode(fileContents)
    const filePointer = malloc(fileBufferBytes.byteLength)

    new Uint8Array(memory.buffer).set(fileBufferBytes, filePointer)

    // Call formatBytes function from WebAssembly
    const resultPointer = formatBytes(filePointer, fileBufferBytes.byteLength, options.indent, options.trailingNewline)

    // Decode the result
    const resultBytes = new Uint8Array(memory.buffer).subarray(resultPointer)
    const end = resultBytes.indexOf(0)
    const result = decoder.decode(resultBytes.subarray(0, end))
    free(filePointer)

    return result
}

export { formatDockerfile, formatDockerfileContents, FormatOptions }
