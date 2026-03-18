# Decision Journal: Native macOS app architecture — Swift/SwiftUI vs Tauri

**Date:** 2026-03-18

## The question

Anito's SPA is embedded in the Go binary. If we distribute Anito as a `.app`, what technology wraps it? The shell needs to handle daemon registration, menu bar presence, and CLI install — all things macOS has specific APIs for.

## Context

The install story was the prompt. The current `docs/setup.md` requires users to: clone the repo, build, copy a binary, create a log directory, copy and edit a plist, and run `launchctl load`. That's six manual steps. The goal was to get it to one: drag to Applications, double-click.

`SMAppService` (macOS 13+) lets an app register a LaunchAgent from inside the `.app` bundle without sudo and without the user touching Terminal. The plist just needs to live at `Contents/Library/LaunchAgents/` inside the bundle. This is exactly how Docker Desktop, Tailscale, and 1Password do it.

## What the exploration revealed

**The shell is thin by design.** The Go binary already does everything — daemon, CLI, MCP, embedded SPA. The `.app` shell only needs to:
1. Register the LaunchAgent (first run)
2. Copy the Go binary to `~/.local/bin/anito`
3. Show a menu bar icon with a status indicator
4. Open `localhost:7700` on click

That's ~200 lines of Swift. No logic, no reimplementation.

**Tauri was the natural first instinct** — it's popular, has a tray plugin, and uses a WebView. But it brings a Rust runtime (~10MB), needs a plugin for `NSStatusBar`, and needs shell-out or a native plugin for `SMAppService`. For a macOS-only tool, none of the cross-platform value applies.

**Swift/SwiftUI is the right call** because:
- `SMAppService` is a 3-line native call
- `NSStatusBar` is built-in
- `WKWebView` is the same engine Tauri uses on macOS anyway
- Bundle is 1–2MB
- Perfect macOS integration — the shell feels invisible

**The install story becomes:**
1. Download `Anito.dmg`
2. Drag `Anito.app` to `/Applications`
3. Double-click
4. First run: "Install background service" button → macOS permission prompt → daemon starts
5. Menu bar icon appears

No Terminal. No plist editing. No `launchctl`. The whole `docs/setup.md` manual process disappears for end users.

## Decision

Swift/SwiftUI for the native shell. See ADR-006.

The minimum viable first pass: menu bar app that detects daemon health (green/red dot), has "Open Dashboard" (opens `localhost:7700` in browser), and has "Install Daemon" (calls `SMAppService`). No WKWebView in the shell itself — the browser handles the dashboard for now.

## What's not decided yet

- Whether the dashboard opens in a WKWebView window inside the app or in the default browser. Browser is simpler for v1; WKWebView feels more native but adds complexity.
- Whether the Swift shell lives in this repo (`cmd/anito-app/`) or a separate repo. Separate keeps the Go build clean; together keeps everything co-located.
- Pricing/distribution model — App Store vs direct download. App Store has sandboxing restrictions that complicate daemon registration. Direct DMG download is simpler.

## Next step

Not yet building this — the Go core needs to be stable first. The Admin SPA (`localhost:7700/admin`) is the prerequisite: the shell needs something worth showing before wrapping it. Revisit when the SPA is production-ready.
