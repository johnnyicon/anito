# What I Actually Want — Senior Site Reliability Engineer
**Role:** I own reliability. When something breaks at 2am, I'm the one who finds out why.
**Format:** No audit. Just what I want.

---

The first thing I do when I open any observability tool is answer one question: is anything on fire right now? The current dashboard cannot answer that in under five seconds. I have to read twelve cards, parse the badge colors, check if anything is red. That is unacceptable for a reliability tool. I want to open this tab and know immediately whether I need to act or whether I can close it.

Give me a status bar at the very top, above everything else. Green = all services healthy. Yellow = one or more warnings. Red = one or more failures or active issues. That's it. One bar, one color, I know the answer in 200 milliseconds. If it's yellow or red, the bar expands or I click it and I see exactly what's wrong: "3 services failed, 1 crash gave up, 2 doctor warnings, 4 unread issues." This is my entry point.

The issues log needs to be first-class. Right now it's a CLI command that nobody runs unless they already know something is wrong. That's backwards. The issues log is the nervous system — it captures every error, every consumer complaint, every failed deploy. I want it surfaced in the dashboard as a live feed. A panel or a section I can always see. Badge count in the header. When a new issue lands, I see it. I don't have to remember to check.

Tell me the story of every service failure. Right now I see "failed" and I open the log panel and start reading. That's fine for a quick crash, but for a crashloop — a service that's failed five times in three minutes — I want to see the timeline. When did it first fail? How many attempts? What was the exit code? Is Anito still trying to restart it or has it given up? "Failed" tells me the outcome. I need the narrative.

I want separate visual language for these situations:
- **Crashing and Anito is retrying**: amber, shows attempt X of 5, shows next retry in Xs
- **Gave up after 5 crashes**: dark red, says "gave up — manual intervention required," tells me the last error
- **Failed to restore at daemon startup**: distinct icon, says "binary missing or start failed at last daemon boot"
- **Health check timeout on deploy**: shows which health check URL, shows how long it waited

These all look the same right now. They are not the same. The recovery action is completely different for each one.

I care about stability trends, not just current state. "Healthy since 4h ago" tells me this service has been stable all morning. "Healthy since 3s ago" tells me it just recovered from something and I should watch it. "Healthy since 3s ago, 4th restart this hour" tells me there's a problem I need to diagnose even if it's currently green. The current dashboard shows me a snapshot. I need a reading of the trajectory.

The SSE log panel needs to tell me when it dropped events. If the stream disconnects and reconnects, I need to see "--- gap: disconnected 14:22:01, reconnected 14:22:04, approximately 3 seconds of output may be missing ---" right in the log. I read logs forensically. Gaps in a forensic record are evidence. I need to know they exist.

I want log correlation across services. If gomanan-mcp crashed at 14:22:01 and I also want to see what gomanan-ui-dev was doing at that exact moment, right now I have to close one log, open another, and squint at timestamps. I want to pin two or three service logs side by side, time-aligned, so I can read across them as events happen. This is the single biggest multiplier on my time-to-diagnose.

The port pressure meter needs to be an alert, not a statistic. I don't want "43 / 101 ports." I want silence when it's fine, and a prominent warning when I'm at 85+ slots. Because the moment auto-allocation starts failing deploys is not the moment I want to find out I was close to the edge.

One more: mutation errors cannot be silent. If I click restart and it fails — for any reason — I need to know. Not "the card un-dims." A visible, dismissable error message. What failed, why it failed, what to do about it. Silent failures in a reliability tool are a category error. The whole point of this thing is to make failures visible.

If this dashboard can do all that, I would use it as my primary reliability surface. Right now it's a reference tool I occasionally glance at. I want it to be the thing I actually watch.
