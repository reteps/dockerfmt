// Assume add.wasm file exists that contains a single function adding 2 provided arguments
import fs from 'node:fs/promises';
// import { console } from 'node:inspector';

import _fs from 'node:fs'
globalThis.fs ??= _fs

console.log('Loading WebAssembly module...');
import './wasm_exec.cjs';

const go = new Go()  // Defined in wasm_exec.js


const wasmBuffer = await fs.readFile('./format.wasm');
// // Use the WebAssembly.instantiate method to instantiate the WebAssembly module
const wasm = await WebAssembly.instantiate(wasmBuffer, go.importObject);

/**
 * Do not await this promise, because it only resolves once the go main()
 * function has exited. But we need the main function to stay alive to be
 * able to call the `parse` and `print` function.
 */
void go.run(wasm.instance)

const { memory, malloc, free, formatBytes } = wasm.instance.exports;

const encoder = new TextEncoder()
const decoder = new TextDecoder()

const line = '  FROM dockerfile\n'
const lineBuffer = encoder.encode(line)
const linePointer = malloc(lineBuffer.byteLength)

new Uint8Array(memory.buffer).set(lineBuffer, linePointer)

// Call formatBytes function from WebAssembly
const resultPointer = formatBytes(linePointer, lineBuffer.byteLength, 4, true)

// Decode the result
const result = new Uint8Array(memory.buffer).subarray(resultPointer)
const end = result.indexOf(0)
const string = decoder.decode(result.subarray(0, end))

// Free the allocated memory
free(linePointer)

// const textPointer = malloc(line.)
// const result = formatBytes(, 4, true)
console.log(string); // Outputs: 11
// console.log(wasmModule);
// // Exported function lives under instance.exports object
// const { FormatFileLines } = exports;

// // Read ../tests/run.dockerfile
// const file = []

// const formatted = FormatFileLines(file, 4, true)

// console.log(formatted); // Outputs: 11