# C6 — `anito doctor` — Consumer Repo Health Check

**What it is:** A CLI command that checks a consuming repo's Anito configuration against the current schema, reports issues, and auto-migrates what it safely can.

**Why it exists:** When Anito's internal schema evolves (e.g., `drain_window` changing from nanosecond Duration to millisecond string, new required fields, deprecated keys), consuming repos shouldn't need to read changelogs or manually update their configs. They run one command.

---

## Usage

```bash
# Run from inside any repo that has .anito/config.yaml (or .anito/*.yaml)
anito doctor

# Output example:
anito doctor — checking .anito/config.yaml

  ✓ name: sogs-api
  ✓ type: binary
  ✓ port: 8080
  ✓ health_check: /health
  ✗ drain_window: "3s" — deprecated string format, now stored as integer milliseconds
    → auto-fixed: drain_window: 3000

  ⚠ watch paths include non-source files:
    - ./internal/ogimage/backgrounds/ contains .png files
    → suggestion: add watch_exclude: ["**/*.png", "**/*.jpg"]

  ✓ service contract: PORT env var read ✓, /health route found ✓

1 issue auto-fixed, 1 warning (manual review suggested)
Write changes? [y/N] y
✓ .anito/config.yaml updated
```

---

## What It Checks

### Schema validation
- All required fields present (`name`, `port` or `stable_port`, `output` or `path`)
- No unknown/deprecated fields (warn, don't error)
- `drain_window` format — detect old nanosecond values (> 1,000,000,000) and offer to convert

### Service contract
- Does the binary or wrapper script exist?
- Does the code read `PORT` from the environment? (static analysis — grep for `os.Getenv("PORT")` or `process.env.PORT`)
- Is there a `/health` route? (or whatever `health_check` says)

### Watch path hygiene
- Are watch paths directories that exist?
- Do any watch paths contain known asset file types (`.png`, `.jpg`, `.gif`, `.svg`, `.mp4`, `.woff`)?
- If so, suggest `watch_exclude` patterns

### Registry alignment (if daemon is running)
- Is this service registered in Anito?
- Does the registered `binary_path` match `config.yaml` `output`?
- Does the registered `stable_port` match `config.yaml` `port`?
- If mismatches: report, offer to fix config to match registry (registry is source of truth for ports)

---

## What It Auto-Fixes

Only changes that are mechanical and unambiguous:
- `drain_window` format conversion (string → ms, detect nanosecond values)
- Deprecated field names (if/when any are renamed)
- Normalize relative vs absolute watch paths

Everything else is reported as a warning with a suggested fix, requiring manual confirmation.

---

## Implementation

Lives in `cmd/anito/` alongside other CLI commands. Reads config files from the current directory (or `--path` flag). Optionally connects to the daemon API to check registry alignment.

```
anito doctor [--path /abs/path/to/repo] [--fix] [--dry-run]
```

`--fix` applies all auto-fixable changes without prompting.
`--dry-run` reports everything but writes nothing.

No new MCP tool — this is a developer-facing CLI operation, not an LLM operation. The LLM's equivalent is `anito_verify`.

---

## Consuming Repo Story

When we ship a breaking config change:
1. We bump the config schema version (already have `schemas/` directory for this)
2. The changelog describes what changed
3. Consuming repo runs `anito doctor` — done in 10 seconds

No consuming repo needs to understand what changed internally. They just run the doctor.
