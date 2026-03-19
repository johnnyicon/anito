# What I Actually Want — Senior Technical Lead
**Role:** Technical lead. I wrote some of the services running in this thing. I use this dashboard daily.
**Format:** No audit. Just what I want.

---

I want to open a browser tab and understand my entire local stack in under three seconds. Not "oh there are 12 services, let me start reading cards." I mean genuinely grok it — what's healthy, what's not, what's been touched recently, where attention is needed. Right now I open the dashboard and it looks like a tile grid of equal-weight cards. Everything looks the same whether one service is silently crashlooping and another has been rock-solid for six hours. That's not a dashboard. That's a list with a border-radius.

The first thing I want is visual triage. Make healthy services quiet. A running service with no issues should take up almost no space — a thin row, a green dot, a name, a port, done. I don't need to read about it. I need to know it's there and working. The services that need my attention should be large, loud, and clearly asking for something. A failed service should be visually invasive — not a red dot on a card that looks exactly like a green card, but something that breaks the visual rhythm and says "this is different, look here."

Keyboard everything. I should be able to hit Cmd+K from anywhere and get a command palette. Type "restart gom" and restart gomanan-mcp. Type "logs sogs" and open the log panel. Type "deploy sogs-api" and trigger the deploy. I should never need to reach for the mouse for operations I do ten times a day. Right now every action requires I find the right card, find the right button, click. For a tool I use constantly, that's too much friction.

I want to know what code is running. "sha:e3141f3d" is useful but buried. I want to see at a glance: is this service running what's on main, or is it on a feature branch, or is it a worktree build? Right now I can see the sha if I set version in the config, but I can't see "this is from a worktree" or "this hasn't been redeployed since before your last merge." Give me that context without me having to remember what I last deployed.

The log panel is the tool I use most, and it's the most limited part of the dashboard. Fixed height, one service, no search, no filter, no tabs. I want a log surface that actually works. Multiple services side by side. Filter by tag (just show me `[ERROR]` and `[CRASH]`). Text search. And importantly: the daemon log and the service logs together, interleaved by timestamp, when I need to correlate "why did this restart at exactly that moment." That's the question I always have and can never answer from the current UI.

I don't want confirmation dialogs for restart. Restart is recoverable. Just do it. If I accidentally restart something, the service comes back up in ten seconds. The cognitive overhead of "confirm restart?" for a reversible action is worse than the cost of an accidental restart. Put undo somewhere (a dismissable toast: "restarted gomanan-mcp — undo?") if you need to cover for mistakes. Confirmation dialogs should be reserved for things that can't be undone: remove, and only remove.

I want the dashboard to know about service dependencies. If services A and B both depend on service C, and I restart C, I want to see "restarting C may affect A and B." I don't want surprises. Anito has the relationship data in `anito_setup` for composite apps. Surface it.

Last thing: I do not want to have to run `anito doctor` from the terminal to know if a service has a config issue. That should be an ambient signal in the dashboard. A small warning indicator on a service card that says "doctor found 1 issue." I click it, I see what the issue is, I see what to do. No terminal, no separate command, no context switch.

The overall feeling I want is: this is a control room, not a gallery. Dense, purposeful, keyboard-driven. Healthy things quiet down. Problems escalate visually. I can drive everything from the keyboard. I never need to open a terminal for routine operations.

That's it. Build that.
