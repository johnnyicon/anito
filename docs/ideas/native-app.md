# Native macOS App (.app Distribution)

A Swift/SwiftUI menu bar shell that wraps the Go binary for drag-to-Applications install. The shell is intentionally thin — all functionality stays in the Go binary.

## What the shell does

- First-run: registers the LaunchAgent via `SMAppService.mainApp.register()` — no sudo, no Terminal, macOS shows its own permission prompt
- Copies `Resources/anito` (the Go binary) to `~/.local/bin/anito` so the CLI works in Terminal
- Lives in the menu bar (not the Dock) with a green/red status dot
- "Open Dashboard" opens `localhost:7700` in the browser
- "Install Daemon" / "Uninstall" for lifecycle management

## Bundle layout

```
Anito.app/
  Contents/
    MacOS/Anito                                    ← Swift shell (~200 lines)
    Resources/anito                                ← Go binary (daemon + CLI + MCP + SPA)
    Library/LaunchAgents/com.anito.daemon.plist    ← SMAppService reads this
```

## Install story

Download DMG → drag to Applications → double-click → one button → done. The entire `docs/setup.md` manual process disappears for end users.

## Why Swift, not Tauri

`SMAppService` and `NSStatusBar` are native APIs; Tauri needs plugins for both. Swift bundle is 1–2MB vs ~10MB+ for Tauri. Anito is macOS-only so cross-platform value doesn't apply. See [ADR-006](../adr/2026-03-18-006-native-app-swiftui-menu-bar.md).

## Open questions

- WKWebView in-app vs browser handoff. Browser is simpler for v1; WKWebView feels more native.
- App Store (sandboxing complications for `SMAppService`) vs direct DMG download.

## Why not yet

The Admin SPA needs to be production-ready first. The shell is the distribution layer — it needs something worth showing. Revisit once the SPA is solid and the tool is used daily.

**Target:** v1.x — after v1 stabilises and the SPA write operations ship.
