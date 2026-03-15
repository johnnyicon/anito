# Anito prompt setup

Save the template below as `ANITO.md` in your project root.
Replace `<service-name>` with the `name:` field from your `anito.yaml`.

If you already have a `CLAUDE.md`, add this one line to it so Claude picks up the context automatically:

```
@ANITO.md
```

Otherwise just reference `ANITO.md` directly when starting a session.

---

## Template — save as `ANITO.md`

```markdown
# Local deployment

This project runs as a persistent local service managed by [anito](https://github.com/johnnyicon/anito).
Anito keeps it always-on at a fixed port, surviving reboots via launchd.

## Deploy

```bash
anito deploy          # build + register + start (reads anito.yaml)
```

Re-deploy any time you want to push a new local build. The port never changes.

## Manage

```bash
anito status <service-name>    # check if it's running and what port it's on
anito restart <service-name>   # restart after a manual binary swap
anito stop <service-name>      # stop without removing from registry
anito services                 # list everything anito is running
```

## Logs

```bash
tail -f ~/.anito/logs/<service-name>.log
```

## If the daemon isn't running

```bash
# Check
curl -s http://localhost:6660/health

# Start it
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist

# Or run it directly (foreground, useful for debugging)
anito daemon
```

## Service contract

This binary:
- Reads `PORT` from the environment and listens on it
- Exposes `GET /health` returning `200 OK`
```
