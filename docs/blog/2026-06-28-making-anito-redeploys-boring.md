# Making Anito Redeploys Boring

Anito is a local production service manager. Its job is to keep a service
available at the same localhost port while the process behind that port changes.
That sounds simple until a redeploy fails halfway through.

The FABLE audit found a subtle reliability problem: Anito could temporarily stop
tracking the old process while trying to start a replacement. If the replacement
failed, the old process might still be serving traffic, but Anito would no
longer have a clean view of it for crash recovery. That is exactly the kind of
"mostly working" state that becomes painful later.

The fix was to make redeploys behave more like a transaction.

Before, the old process was removed from the manager early. Now it is detached:
still running, but held aside while the replacement proves itself. The new
process starts on Anito-assigned internal port(s), passes its health check, and
then Anito verifies that those internal listener ports are actually owned by the
new process or one of its children. Only after that does the proxy swap to the
new process and drain the old one.

If anything fails before the proxy swap, Anito restores the old process and the
old registry record. From the developer's point of view, the last good version
keeps serving.

This matters because Anito's promise is not just "restart my process." Its
promise is "keep my local stack stable while I work." A bad build, a failed
health check, or a local port race should not leave the service in a half-known
state.

The FABLE hardening pass also tightened nearby surfaces:

- management, proxy, and MCP HTTP servers now have read timeouts to resist slow
  clients while keeping streaming writes open;
- registry updates during deploy/restart are grouped so hot paths do less
  repeated file rewriting;
- tests now cover failed replacement restore, process crash tracking after
  restore, listener ownership verification, and timeout configuration.

The result is not flashy. That is the point. The best redeploy is boring: start
the candidate, prove it is the right process, swap the proxy, then retire the
old one. If the candidate fails, keep serving the known-good process and tell
the operator what happened.
