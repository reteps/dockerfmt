// Go's wasm_exec.js needs globalThis.crypto.getRandomValues, which Node only
// provides without a flag from version 19 on. This module must be imported
// before wasm_exec.js, which checks for it when it is evaluated.
import { webcrypto } from 'node:crypto'

if (typeof globalThis.crypto?.getRandomValues !== 'function') {
    Object.defineProperty(globalThis, 'crypto', {
        value: webcrypto,
        configurable: true,
        enumerable: true,
        writable: true,
    })
}
