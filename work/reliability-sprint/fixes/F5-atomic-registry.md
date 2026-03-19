# F5 — Non-Atomic Registry Writes

**Finding:** `internal/registry/registry.go` writes with `os.WriteFile()` directly to `registry.json`. On daemon crash mid-write, the file can be partially written and unparseable on the next startup, causing all services to fail to restore.

---

## Risk Level

**Low probability, high impact.** On APFS (macOS default), small files written via `os.WriteFile()` are often copy-on-write and thus effectively atomic. But:
- Registry JSON grows with each service (~200 bytes/service; currently ~2.6KB for 13 services)
- Under memory pressure or unusual conditions, the write can be interrupted
- There is no recovery path if `registry.json` is malformed — all services fail to restore

---

## Fix

Replace `os.WriteFile()` with an atomic temp-file + rename pattern:

```go
func (r *Registry) save() error {
    data, err := json.MarshalIndent(r.services, "", "  ")
    if err != nil {
        return err
    }
    tmp := r.path + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, r.path)  // atomic on APFS and ext4
}
```

`os.Rename()` is atomic within the same filesystem on both macOS (APFS) and Linux (ext4/btrfs). The registry file either has the complete new content or the previous content — never a partial write.

---

## Additional: Registry Backup

Consider keeping `registry.json.bak` (the previous-good version) alongside the primary:

```go
os.Rename(r.path, r.path+".bak")  // preserve last-good
os.Rename(tmp, r.path)
```

On startup, if `registry.json` is malformed, fall back to `registry.json.bak` with a log warning. This makes the daemon self-healing on the one failure mode that currently causes total service loss.

---

## Files to Touch

- `internal/registry/registry.go` — `save()` function only
- `cmd/anito/main.go` — startup: add fallback to `.bak` if primary parse fails

---

## Test Coverage Needed

- Registry saves correctly → reads back identical
- Registry backup exists after save → `.bak` contains previous version
- Startup with malformed primary + valid `.bak` → services restored from backup, warning logged
