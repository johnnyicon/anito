# What I Actually Want — Senior DevOps Engineer
**Role:** I own the deploy pipeline. I run `anito deploy` more than any other command.
**Format:** No audit. Just what I want.

---

The deploy loop is missing from the dashboard and that's the first thing I'd fix. My workflow is: write code, build, deploy, check health, check logs, decide if it worked. Right now I do the first three steps in the terminal and then switch to the browser for the last two. That split is unnecessary friction. I want to close that loop in the browser.

What I want for deploy: a "Redeploy" button per service. Click it. The dashboard runs the build command from the config (if there is one), streams the build output live in the log panel, then does the health-check-and-swap. I watch it happen. Green checkmark when the swap is done. Build failed? I see exactly why without opening a terminal. Health check failed? I see the timeout and the last log lines from the service before the check gave up. The whole story, in one place.

Build output is just as important as runtime logs. When a deploy fails, the reason is almost always in the build output — a compilation error, a missing dependency, a failed test. Right now that output lives in the terminal and disappears when the window closes. I want the last build output stored per service and accessible from the dashboard. "Last build output" as a tab in the log panel, right next to the live log.

Watch mode deserves its own UI treatment. Right now it's a badge. An amber dot. That's it. Watch mode is one of the most useful features in Anito — file changes automatically trigger a restart — but the dashboard gives it almost no surface. I want:
- The list of watched paths visible without opening a separate tool
- "Last triggered by: /src/handlers/api.go (14:22:03)" — what file changed, when
- A graph or list of recent restarts with their trigger file
- A way to temporarily disable watch mode from the UI without redeploying

When a service is in watch mode and it's restarting constantly because of a bad file pattern, I need to diagnose it fast. Right now I `anito logs daemon` and grep for `[WATCH]`. That works but it's friction. Give me that surface in the UI.

I want to see the config that produced this service. Click a service, see the `.anito/config.yaml` content — read-only is fine. I constantly forget what `build:` command is set, what `health_check_timeout` is, what `drain_window` is. Right now I either remember or I open a file explorer. The config is in the registry — `config_path` is stored now — so just render it.

Env file visibility. I don't need to see the values — that would be a security issue even locally — but I want to see the keys. "This service is running with: PORT, DATABASE_URL, REDIS_URL, JWT_SECRET (4 vars, from /path/to/.anito/ports.env)." That lets me quickly verify "is the env file being loaded?" and "does it have the variable I think it has?" without `cat`ing files.

Port management should be proactive. I want to see: which ports are in use, by which service, and whether any of them are currently experiencing a conflict. Port 5173 being shadowed by an old Vite process is the kind of thing that costs me twenty minutes to debug. Doctor catches it, but I have to run doctor to find out. Put a persistent port health check in the dashboard. If port X has a foreign process on it, flag it on the service card, not just in a doctor report I have to request.

Multi-service operations. I run `anito deploy` across three related services when I'm doing a combined release. I'd love to select multiple services in the dashboard and hit "redeploy all selected." Or at minimum, "restart all failed." Right now it's three terminal windows or three CLI commands. That's manageable at three but annoying at seven.

The version story needs to be better. Right now I set `version: sha:abc123` in the config and it shows up as text on the card. What I actually want is: show the git commit message for that SHA, show when it was built (from file timestamp or build command output), show whether the binary is newer or older than the last git commit in the repo. That last one is critical: "this service's binary is from a commit that's 3 commits behind HEAD" is a thing I want to know without running `git log`.

Last thing, and I feel strongly about this: the dashboard should be the place I go to understand what's running, not the place I go after I already know something broke. Right now it's the latter. It doesn't push information at me — I have to pull it. Fix that and you have a tool I'd actually keep open all day.
