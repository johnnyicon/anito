# Anito prompt setup

Save the template below as `ANITO.md` in your project root.
Replace `<service-name>` and `<port>` with the values from your `.anito/config.yaml`.

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
Anito proxies a stable port to the process, so consumers always connect to the same address
even across deploys. Re-deploying is zero-downtime: the new binary is health-checked before
traffic switches over.

## Deploy

```bash
anito deploy          # reads .anito/config.yaml — builds, starts, health-checks, swaps proxy
```

The service will be live at `localhost:<port>` after this completes.

## Manage

```bash
anito status <service-name>    # port, status, PID, last deploy time
anito restart <service-name>   # restart with health-check gating
anito stop <service-name>      # stop without removing from registry
anito services                 # list everything anito is managing
```

## Logs

```bash
anito logs <service-name>            # last 100 lines
tail -f ~/.anito/logs/<service-name>.log   # live tail
```

## If the daemon isn't running

```bash
# Check
curl -s http://localhost:7700/health

# Start it
launchctl load ~/Library/LaunchAgents/com.anito.daemon.plist

# Or run it directly (foreground, useful for debugging)
anito daemon
```

## Service contract

This binary:
- Reads `PORT` (or `PORT_<NAME>` for multi-port services) from the environment and listens on it (Anito injects ephemeral port(s))
- Exposes `GET /health` returning `200 OK`
```
