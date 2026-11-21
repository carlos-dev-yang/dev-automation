#!/usr/bin/env node
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, "..")
const skillsDir = path.join(repoRoot, "skills")

const KEBAB_CASE_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/
const VALID_STATUSES = new Set(["official", "draft", "deprecated"])
const RECOMMENDED_MAX_LINES = 150

function extractFrontmatter(content) {
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/)
  if (!match) return null
  return { raw: match[1], body: content.slice(match[0].length) }
}

function parseSimpleYamlField(raw, key) {
  const regex = new RegExp(`^${key}:\\s*(.*)$`, "m")
  const match = raw.match(regex)
  if (!match) return null
  const value = match[1].trim()
  if (value === ">-" || value === ">" || value === "|-" || value === "|") {
    const startIdx = match.index + match[0].length
    const rest = raw.slice(startIdx).split(/\r?\n/)
    const lines = []
    for (const line of rest) {
      if (line === "") {
        if (lines.length > 0) lines.push("")
        continue
      }
      if (/^\s+/.test(line)) {
        lines.push(line.trim())
      } else {
        break
      }
    }
    return lines.join(" ").trim()
  }
  return value.replace(/^["']|["']$/g, "")
}

function parseMetadataStatus(raw) {
  const match = raw.match(/^metadata:\s*\r?\n([\s\S]*?)(?=^[a-zA-Z0-9_-]+:|\s*$)/m)
  if (!match) return null
  const statusMatch = match[1].match(/^\s+status:\s*([^\s#]+)/m)
  return statusMatch ? statusMatch[1].replace(/^["']|["']$/g, "") : null
}

function checkRelativeLinks(skillFile, body) {
  const broken = []
  const linkRe = /\[[^\]]+\]\(([^)]+)\)/g
  for (const match of body.matchAll(linkRe)) {
    const target = match[1].trim()
    if (
      target.startsWith("http://") ||
      target.startsWith("https://") ||
      target.startsWith("mailto:") ||
      target.startsWith("#")
    ) {
      continue
    }
    const cleanTarget = target.split("#")[0]
    if (!cleanTarget) continue
    const resolved = path.resolve(path.dirname(skillFile), cleanTarget)
    if (!fs.existsSync(resolved)) {
      broken.push(target)
    }
  }
  return broken
}

if (!fs.existsSync(skillsDir)) {
  console.error(`skills/ directory not found at ${skillsDir}`)
  process.exit(1)
}

const entries = fs
  .readdirSync(skillsDir, { withFileTypes: true })
  .filter((entry) => !entry.name.startsWith("."))
  .sort((a, b) => a.name.localeCompare(b.name))

const errors = []
const warnings = []
const summary = []

for (const entry of entries) {
  if (!entry.isDirectory() && !entry.isSymbolicLink()) {
    errors.push(`Unexpected non-directory in skills/: ${entry.name}`)
    continue
  }

  const skillName = entry.name
  const skillPath = path.join(skillsDir, skillName)
  const skillMdPath = path.join(skillPath, "SKILL.md")

  if (!KEBAB_CASE_RE.test(skillName)) {
    errors.push(`${skillName}: folder name must be lowercase kebab-case`)
  }

  if (!fs.existsSync(skillMdPath)) {
    errors.push(`${skillName}: missing SKILL.md`)
    continue
  }

  const content = fs.readFileSync(skillMdPath, "utf8")
  const fm = extractFrontmatter(content)
  if (!fm) {
    errors.push(`${skillName}: SKILL.md missing valid YAML frontmatter`)
    continue
  }

  const name = parseSimpleYamlField(fm.raw, "name")
  const description = parseSimpleYamlField(fm.raw, "description")
  const status = parseMetadataStatus(fm.raw)

  if (!name) {
    errors.push(`${skillName}: frontmatter missing 'name'`)
  } else if (name !== skillName) {
    errors.push(`${skillName}: frontmatter name '${name}' does not match folder name`)
  }

  if (!description) {
    errors.push(`${skillName}: frontmatter missing 'description'`)
  }

  if (status) {
    if (!VALID_STATUSES.has(status)) {
      errors.push(`${skillName}: invalid metadata.status '${status}' (expected official|draft|deprecated)`)
    }
    if (status === "draft" && description && !description.includes("[DRAFT]")) {
      warnings.push(`${skillName}: metadata.status is 'draft' but description lacks '[DRAFT]' prefix`)
    }
  } else {
    warnings.push(`${skillName}: metadata.status is not set (recommended: official|draft)`)
  }

  const brokenLinks = checkRelativeLinks(skillMdPath, fm.body)
  for (const broken of brokenLinks) {
    errors.push(`${skillName}: broken relative link '${broken}' in SKILL.md`)
  }

  const lineCount = content.split(/\r?\n/).length
  if (lineCount > RECOMMENDED_MAX_LINES) {
    warnings.push(
      `${skillName}: SKILL.md has ${lineCount} lines (recommended <= ${RECOMMENDED_MAX_LINES}; consider moving details to references/)`,
    )
  }

  summary.push({
    name: skillName,
    status: status ?? "unspecified",
    lines: lineCount,
  })
}

console.log(`Validated ${summary.length} skill(s) in ${skillsDir}:`)
for (const item of summary) {
  console.log(`  - ${item.name} [status=${item.status}, lines=${item.lines}]`)
}

if (warnings.length > 0) {
  console.log("\nWarnings:")
  for (const w of warnings) console.log(`  ! ${w}`)
}

if (errors.length > 0) {
  console.error("\nErrors:")
  for (const e of errors) console.error(`  x ${e}`)
  process.exit(1)
}

console.log("\nAll skills passed validation.")
