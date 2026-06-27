// Pins the main package, every platform sub-package, and the main package's
// optionalDependencies to a single version. Run at publish time so the launcher
// always pulls in the exact-matching platform binary and the published versions
// can never drift apart.
//
//   node scripts/set-version.mjs <version>
import { readFileSync, writeFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const version = process.argv[2]
if (!version) {
    console.error('usage: node scripts/set-version.mjs <version>')
    process.exit(1)
}

const jsDir = dirname(dirname(fileURLToPath(import.meta.url)))
const npmDir = join(jsDir, 'npm')

const writeJson = (path, obj) =>
    writeFileSync(path, JSON.stringify(obj, null, 2) + '\n')

// Platform sub-packages.
const optionalDependencies = {}
for (const name of readdirSync(npmDir).sort()) {
    const pkgPath = join(npmDir, name, 'package.json')
    const pkg = JSON.parse(readFileSync(pkgPath, 'utf8'))
    pkg.version = version
    writeJson(pkgPath, pkg)
    optionalDependencies[pkg.name] = version
}

// Main package: bump version and pin every optional dependency exactly.
const mainPath = join(jsDir, 'package.json')
const main = JSON.parse(readFileSync(mainPath, 'utf8'))
main.version = version
main.optionalDependencies = optionalDependencies
writeJson(mainPath, main)

console.log(
    `Pinned ${version} on main + ${Object.keys(optionalDependencies).length} platform packages`,
)
