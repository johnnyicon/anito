# Schema Versioning Pre-Commit Hook

**Status:** Schema files are in place (`schemas/setup-state.v1.json`, `schemas/setup-state-migrations.json`). Hook enforcement is parked. See ADR-006 for the full schema design and migration registry pattern.

## The idea

A git pre-commit hook that detects changes to files in `schemas/` and automatically:
1. Bumps the `schemaVersion` field in the affected schema file
2. Appends an entry to a human-readable migration log (`schemas/CHANGELOG.md`)
3. Fails the commit if the version was not bumped

## Why

The `setup-state.json` written to consuming repos contains a `schemaVersion` field. When Anito runs setup on a repo that has a state file from an older version, it needs to know what changed. Without a versioned migration log, there's no way to know whether an older state file is still valid or needs migration steps.

## How it would work

1. Pre-commit hook runs `git diff --name-only --cached | grep schemas/`
2. If schema files changed, check whether the `$id` version was also bumped
3. If not bumped, prompt or auto-bump the patch version
4. Append a `schemas/CHANGELOG.md` entry: date, from-version, to-version, summary of what changed, whether it's breaking, whether it's auto-migratable
5. Stage the bumped schema + changelog entry before allowing the commit to proceed

**Dependencies:** A hook runner (`scripts/pre-commit` or `lefthook`).

**Target:** v1.x maintenance — low priority, build when schema churn becomes a real problem.
