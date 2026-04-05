// anito-notify — lightweight macOS notification helper for Anito.
//
// Usage:
//   anito-notify --title "Anito" --message "sogs-api deployed" [--sound]
//
// Must be run from within AnitoNotify.app bundle for notification permissions.
// On first run, macOS will prompt to allow notifications.

import AppKit
import UserNotifications

// ── Parse arguments ─────────────────────────────────────────────────────────

func arg(_ flag: String) -> String? {
    guard let idx = CommandLine.arguments.firstIndex(of: flag),
          idx + 1 < CommandLine.arguments.count else { return nil }
    return CommandLine.arguments[idx + 1]
}

func hasFlag(_ flag: String) -> Bool {
    CommandLine.arguments.contains(flag)
}

let title   = arg("--title") ?? "Anito"
let message = arg("--message") ?? ""
let sound   = hasFlag("--sound")

guard !message.isEmpty else {
    fputs("usage: anito-notify --title TITLE --message MSG [--sound]\n", stderr)
    exit(1)
}

// ── App delegate that sends the notification and exits ──────────────────────

class NotifyDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        let center = UNUserNotificationCenter.current()

        center.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
            if let error = error {
                fputs("auth error: \(error.localizedDescription)\n", stderr)
                NSApp.terminate(nil)
                return
            }
            guard granted else {
                fputs("notifications not allowed\n", stderr)
                NSApp.terminate(nil)
                return
            }

            let content = UNMutableNotificationContent()
            content.title = title
            content.body = message
            if sound {
                content.sound = .default
            }

            let request = UNNotificationRequest(
                identifier: UUID().uuidString,
                content: content,
                trigger: nil
            )

            center.add(request) { error in
                if let error = error {
                    fputs("send error: \(error.localizedDescription)\n", stderr)
                }
                // Give the notification a moment to display, then exit
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    NSApp.terminate(nil)
                }
            }
        }
    }
}

// ── Launch as a proper NSApplication (required for notification permissions) ─

let delegate = NotifyDelegate()
let app = NSApplication.shared
app.delegate = delegate
app.setActivationPolicy(.accessory) // no dock icon, no menu bar
app.run()
