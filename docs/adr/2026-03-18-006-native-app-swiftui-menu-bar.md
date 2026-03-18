# ADR-006: Native macOS app uses Swift/SwiftUI menu bar shell, not Tauri

**Date:** 2026-03-18
**Status:** Accepted
**Tags:** distribution, native-app, swift, tauri, menu-bar

## Context

Anito is macOS-only by design. The SPA is already embedded in the Go binary and served at `localhost:7700`. For distribution as a `.app`, a native shell is needed to:

1. Register the launchd daemon on first run (no Terminal required)
2. Copy the CLI binary to `$PATH`
3. Live in the macOS menu bar (not the Dock)
4. Provide a way to open the dashboard

Two options were evaluated: **Tauri** (Rust + WebView, cross-platform) and **Swift/SwiftUI** (native macOS).

## Decision

Use **Swift/SwiftUI** for the native shell.

The shell is intentionally thin — it does not reimplement any Anito functionality. It:
- Detects whether the daemon is running (`GET localhost:7700/health`)
- Registers the LaunchAgent via `SMAppService.mainApp.register()` on first run
- Copies `Resources/anito` (the Go binary) to `~/.local/bin/anito`
- Shows a menu bar icon with status indicator and "Open Dashboard" item
- Opens `localhost:7700` in the browser (or a WKWebView window)

The `.app` bundle layout:
```
Anito.app/
  Contents/
    MacOS/Anito                                    ← Swift shell binary
    Resources/anito                                ← Go binary (daemon + CLI + MCP + SPA)
    Library/LaunchAgents/com.anito.daemon.plist    ← SMAppService reads this
    Info.plist
```

## Why not Tauri

| Concern | Tauri | Swift/SwiftUI |
|---------|-------|---------------|
| Bundle size | ~10MB+ (Rust runtime) | ~1–2MB |
| `SMAppService` | Requires shell-out or plugin | Native, 3 lines |
| `NSStatusBar` (menu bar) | Plugin required | Built-in |
| macOS feel | Good | Perfect |
| Cross-platform | Yes (irrelevant for Anito) | macOS only |

Since Anito is explicitly a macOS-only tool, cross-platform capability is not a benefit. Swift gives direct access to all macOS system APIs needed for the install story with no abstraction layer.

## Consequences

**Positive:**
- `SMAppService` handles daemon registration with no sudo, no Terminal, no manual plist copying
- Full first-run install flow: drag to Applications → launch → click one button → done
- Menu bar app is the right UX for a background service manager
- Tiny bundle — the Swift shell is ~200 lines

**Negative:**
- Requires Xcode to build the Swift shell (separate from the Go build)
- Two build steps to produce the final `.app` (Go binary + Swift shell)
- Swift/Xcode toolchain is an additional dependency for contributors building from source

**Not yet built** — this is a directional decision. The Go binary and daemon are the priority. The Swift shell is the distribution layer once the core is stable.
