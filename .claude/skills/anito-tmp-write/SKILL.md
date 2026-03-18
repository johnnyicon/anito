---
description: "Write content to the Anito project's local tmp folder. Use when the user says 'write to our local temp folder', 'to our local tmp', 'put that in our local tmp', 'save that to our local temp', 'drop that in our local tmp', 'write this to local tmp', 'write that locally to tmp', or any variation meaning save/write something to the anito project's temporary folder. The tmp folder is at /Users/kanekoa/Workspace/anito/tmp/ and is gitignored — files written here are always local-only."
---

# Anito Local Tmp Write

Write content to `/Users/kanekoa/Workspace/anito/tmp/`.

## Steps

1. **Determine the filename** — use a short descriptive slug + today's date in `YYYY-MM-DD` format.
   - Format: `<descriptive-slug>-YYYY-MM-DD.<ext>`
   - Examples: `watch-mode-notes-2026-03-17.md`, `schema-draft-2026-03-17.json`
   - Default extension is `.md` unless the content clearly calls for another format

2. **Write the file** using the Write tool to `/Users/kanekoa/Workspace/anito/tmp/<filename>`

3. **Confirm** — tell the user the filename it was written to.

## Notes

- All files go into `/Users/kanekoa/Workspace/anito/tmp/` — no subdirectories
- The folder's `.gitignore` ignores everything except itself — nothing written here will ever be committed
- If the user specifies a filename, use it exactly; otherwise generate one from context
