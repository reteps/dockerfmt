#!/usr/bin/env node
// Thin launcher that execs the real `dockerfmt` Go binary. There is no JS
// reimplementation of the CLI: the binary is built from cmd/, so flags,
// defaults, EditorConfig handling and exit codes are guaranteed to match the
// standalone Go tool exactly. Each platform's binary ships in its own optional
// dependency (see optionalDependencies in package.json); npm installs only the
// one matching the host's os/cpu.
import { spawnSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { constants } from 'node:os'

const require = createRequire(import.meta.url)

// Keep this list in sync with the directories under js/npm/ and the build
// matrix in release-js.yaml.
const supported = ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64']

const fail = (message: string): never => {
    process.stderr.write(`dockerfmt: ${message}\n`)
    process.exit(1)
}

const target = `${process.platform}-${process.arch}`
const pkg = `@reteps/dockerfmt-${target}`

if (!supported.includes(target)) {
    fail(
        `no prebuilt binary for ${target} (available: ${supported.join(', ')}).\n` +
            'Install it with `go install github.com/reteps/dockerfmt@latest` instead.',
    )
}

let binPath = ''
try {
    binPath = require.resolve(`${pkg}/bin/dockerfmt`)
} catch {
    fail(
        `the platform package ${pkg} is not installed. It is an optional ` +
            'dependency of @reteps/dockerfmt, so it is skipped by installs ' +
            `that omit optional dependencies; install it with \`npm install ${pkg}\`.`,
    )
}

const result = spawnSync(binPath, process.argv.slice(2), { stdio: 'inherit' })

if (result.error) {
    const { code, message } = result.error as NodeJS.ErrnoException
    fail(`could not run ${binPath}: ${code ?? message}`)
}

if (result.signal) {
    // Die from the same signal so the caller sees how the binary ended. If
    // this process survives it (Node ignores SIGPIPE, for example), exit with
    // the shell's 128+n convention instead.
    process.kill(process.pid, result.signal)
    process.exit(128 + constants.signals[result.signal])
}

process.exit(result.status ?? 1)
