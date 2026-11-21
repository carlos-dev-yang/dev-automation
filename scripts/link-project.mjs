#!/usr/bin/env node
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, "..")
const centralSkillsDir = path.join(repoRoot, "skills")

function parseArgs(argv) {
  let projectDir = null
  let dryRun = false
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i]
    if (arg === "--project" && argv[i + 1]) {
      projectDir = path.resolve(argv[i + 1])
      i += 1
    } else if (arg === "--dry-run") {
      dryRun = true
    } else if (arg === "--help" || arg === "-h") {
      console.log(
        "Usage: node scripts/link-project.mjs --project <absolute-or-relative-path> [--dry-run]",
      )
      process.exit(0)
    }
  }
  return { projectDir, dryRun }
}

const { projectDir, dryRun } = parseArgs(process.argv.slice(2))

if (!projectDir) {
  console.error("Missing required argument: --project <path>")
  process.exit(1)
}

if (!fs.existsSync(projectDir)) {
  console.error(`Project directory does not exist: ${projectDir}`)
  process.exit(1)
}

const targets = [
  path.join(projectDir, ".agents", "skills"),
  path.join(projectDir, ".claude", "skills"),
]

let hasBlocker = false

for (const linkPath of targets) {
  const parentDir = path.dirname(linkPath)
  const relTarget = path.relative(parentDir, centralSkillsDir)

  if (fs.existsSync(linkPath) || fs.lstatSync(linkPath, { throwIfNoEntry: false })) {
    const stat = fs.lstatSync(linkPath)
    if (stat.isSymbolicLink()) {
      const currentTarget = fs.readlinkSync(linkPath)
      const resolvedCurrent = path.resolve(parentDir, currentTarget)
      if (resolvedCurrent === centralSkillsDir) {
        console.log(`[OK] Already linked: ${linkPath} -> ${currentTarget}`)
        continue
      }
      console.error(
        `[CONFLICT] Existing symlink points elsewhere: ${linkPath} -> ${currentTarget} (expected ${relTarget})`,
      )
      hasBlocker = true
      continue
    }

    console.error(
      `[CONFLICT] Real directory/file already exists at ${linkPath}. Migrate or back up its contents into ${centralSkillsDir} before linking.`,
    )
    hasBlocker = true
    continue
  }

  if (dryRun) {
    console.log(`[DRY-RUN] Would create symlink: ${linkPath} -> ${relTarget}`)
  } else {
    fs.mkdirSync(parentDir, { recursive: true })
    fs.symlinkSync(relTarget, linkPath, "dir")
    console.log(`[LINKED] ${linkPath} -> ${relTarget}`)
  }
}

if (hasBlocker) {
  console.error("\nStopped due to existing path conflicts. No files were deleted or overwritten.")
  process.exit(1)
}
