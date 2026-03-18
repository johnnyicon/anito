# Discovering the tahua-www embedded SPA architecture

- **ID:** 019d00f2-fd3d-74da-8fa4-5024f76e4bd2
- **Short ID:** 019d00f2
- **Date:** 2026-03-18

## Journey

User reported that editing tahua-web source files was not hot-reloading at port 8104. Initial hypothesis: watch_paths not set. After setting watch_paths, still no [WATCH] events. Dug into the Go source and found //go:embed dist in cmd/server/main.go — the entire frontend is baked into the binary at compile time via make web (cd ../tahua-web && npm run build && cp -r dist ../tahua-www/cmd/server/dist). The anito_setup tool had correctly identified this pattern and set watch paths on cmd/ and internal/ (Go source), not on apps/tahua-web/src/. Nothing was wrong with Anito or the setup. The architecture itself is the explanation: port 8104 serves a frozen snapshot, port 8103 is the live Vite dev server.

## What It Revealed

anito_setup correctly reads the repo and reflects the architecture it finds. The embedded SPA pattern (//go:embed + manual make web build step) is a production assembly pattern, not a dev loop pattern. The setup tool should surface a warning when it detects this pattern so developers know upfront that port 8103 (Vite) is the dev URL and port 8104 is the integration/production URL. This is a gap in anito_setup output, not a misconfiguration.
