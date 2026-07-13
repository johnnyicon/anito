# CI and Release Gates

Anito's GitHub workflows run on `macos-15` because the project is explicitly macOS-first and the release artifacts are Darwin binaries.

The repo-local entrypoint is:

```bash
bash scripts/ci/check pr
```

That PR gate runs the same deterministic checks used in GitHub Actions:

- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go vet ./...`
- `TIMEOUT=180s CHECK=1 bash scripts/coverage`
- `npm run build` in `internal/server/ui`
- `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...`

The release gate adds tagged Darwin builds and checksums:

```bash
bash scripts/ci/check release v0.1.0
```

To validate packaging without running the full PR gate first:

```bash
bash scripts/ci/check release-build v0.1.0
```

Artifacts are written to `dist/release/` as:

- `anito_<version>_darwin_arm64`
- `anito_<version>_darwin_amd64`
- `SHA256SUMS.txt`
