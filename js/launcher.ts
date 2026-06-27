#!/usr/bin/env node
// Thin launcher that execs the real `dockerfmt` Go binary. There is no JS
// reimplementation of the CLI: the binary is built from cmd/root.go, so flags,
// defaults, EditorConfig handling and exit codes are guaranteed to match the
// standalone Go tool exactly. Each platform's binary ships in its own optional
// dependency (see optionalDependencies in package.json); npm installs only the
// one matching the host's os/cpu.
import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)

const platform = process.platform
const arch = process.arch

// Keep this list in sync with optionalDependencies in package.json, the
// directories under js/npm/, and the build matrix in release-js.yaml.
const pkg = `@reteps/dockerfmt-${platform}-${arch}`
const binName = platform === 'win32' ? 'dockerfmt.exe' : 'dockerfmt'

let binPath: string
try {
    binPath = require.resolve(`${pkg}/bin/${binName}`)
} catch {
    throw new Error(
        `dockerfmt does not ship a prebuilt binary for ${platform}-${arch} ` +
            `(expected optional dependency "${pkg}"). Supported platforms: ` +
            `linux-x64, linux-arm64, darwin-x64, darwin-arm64, win32-x64. ` +
            `Download the Go binary from ` +
            `https://github.com/reteps/dockerfmt/releases instead.`,
    )
}

try {
    execFileSync(binPath, process.argv.slice(2), { stdio: 'inherit' })
} catch (err: unknown) {
    const status = (err as { status?: number; signal?: string })?.status
    // Propagate the binary's own exit code (e.g. 1 from --check) so the launcher
    // is behaviourally transparent.
    process.exit(typeof status === 'number' ? status : 1)
}
