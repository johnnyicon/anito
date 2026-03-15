# Anito CLAUDE.md setup snippet

Copy the block below into your project's `CLAUDE.md` (create one if it doesn't exist).
Replace `<service-name>` with the `name:` field from your `anito.yaml`.

---

```markdown
## Local deployment

This project runs as a persistent local service managed by [anito](https://github.com/johnnyicon/anito).
Anito keeps it always-on at a fixed port, surviving reboots via launchd.

### Deploy

```bash
anito deploy          # build + register + start (reads anito.yaml)
```

Re-deploy any time you want to ship a new local build. The port never changes.

### Manage

```bash
anito status <service-name>    # check if it's running and what port it's on
anito restart <service-name>   # restart after a manual binary swap
anito stop <service-name>      # stop without removing from registry
anito services                 # list everything anito is running
```

### Logs

```bash
tail -f ~/.anito/logs/<service-name>.log
```

### If the daemon isn't running

```bash
# Check
curl -s http://localhost:6660/health

# Start it
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist

# Or run it directly (foreground, useful for debugging)
anito daemon
```

### Service contract

This binary must:
- Read `PORT` from the environment and listen on it
- Expose `GET /health` returning `200 OK`
```
