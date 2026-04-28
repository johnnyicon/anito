# Feature Request: Configurable Proxy Bind Address for Tailnet Access

## Summary

Allow Anito's stable proxy listeners to bind to a configurable address instead of always binding to `localhost`.

This would let a service managed by Anito remain internally private on its ephemeral localhost port while exposing only the stable Anito proxy port on a Tailscale address. The day-to-day result is that a service running on one Mac can be opened from another tailnet device at a stable URL such as:

```text
http://johns-macbook-pro:5174
```

## Problem

Anito currently owns stable service ports by creating proxy listeners on localhost:

```go
net.Listen("tcp", fmt.Sprintf("localhost:%d", stablePort))
```

That works well for same-machine consumers, but it blocks a common local-network workflow:

1. A service is managed by Anito on `johns-macbook-pro`.
2. The stable port is `5174`.
3. Another trusted machine on the same tailnet wants to open the app in a browser.
4. `http://johns-macbook-pro:5174` fails because the proxy only listens on loopback.

The current workaround is SSH local forwarding:

```bash
ssh -N -L 5174:127.0.0.1:5174 johns-macbook-pro
```

That is acceptable for one-off access, but it is cumbersome as a day-to-day workflow and does not match Anito's direction as a stable local service gateway.

## Desired Behavior

Anito should support a configurable proxy bind address, with `localhost` preserved as the default.

Example daemon-level configuration:

```yaml
proxy_bind_address: 100.94.58.29
```

or service-level configuration:

```yaml
name: gomanan-ui-dev
port: 5174
proxy_bind_address: 100.94.58.29
```

Then Anito would listen on:

```text
100.94.58.29:5174
```

while continuing to route to the managed process on its internal localhost-only ephemeral port:

```text
100.94.58.29:5174 -> localhost:<internal_port>
```

## Non-Goals

- Do not require managed services to bind to Tailscale themselves.
- Do not expose internal ephemeral service ports.
- Do not change the `PORT` contract for managed services.
- Do not default to public/network binding.
- Do not require service-specific framework flags such as Vite `--host`.

## Recommended Design

Keep managed processes private and only expose the stable proxy listener.

```text
remote app process -> localhost:<ephemeral internal port>
Anito proxy        -> <configured bind address>:<stable port>
consumer browser   -> http://<tailnet host>:<stable port>
```

This preserves Anito's core invariant: consumers connect to the stable proxy port, while the underlying process can restart on new ephemeral ports without consumers noticing.

## Configuration Shape

Recommended minimum:

```yaml
proxy_bind_address: localhost
```

The default must remain `localhost` for backward compatibility.

Useful follow-up options:

- `proxy_bind_address: 100.94.58.29` for tailnet-only access on a specific machine.
- `proxy_bind_address: 0.0.0.0` for explicit all-interface development access.
- `proxy_public_url: http://johns-macbook-pro:5174` if Anito should display or return a friendlier address than the raw bind IP.

## Implementation Notes

The key code path appears to be `internal/proxy.Manager.Register`, which currently accepts only a service name and stable port. It should likely accept a bind address as well:

```go
func (m *Manager) Register(name string, stablePort int, bindAddress string) error
```

The listener address would become:

```go
addr := net.JoinHostPort(bindAddress, strconv.Itoa(stablePort))
l, err := net.Listen("tcp", addr)
```

Use `net.JoinHostPort` rather than string formatting so IPv6 addresses are handled correctly.

The upstream target should remain localhost:

```go
target, err := url.Parse(fmt.Sprintf("http://localhost:%d", internalPort))
```

That keeps the managed process private even when the stable proxy is reachable over Tailscale.

## API and UI Implications

Anito currently reports pinned addresses as localhost URLs in several places. Those should be updated once a bind address or public URL is configured.

Expected examples:

```json
{
  "stable_port": 5174,
  "address": "http://johns-macbook-pro:5174",
  "bind_address": "100.94.58.29"
}
```

The dashboard's external-link buttons should also use the configured display/public address when available instead of always linking to `http://localhost:<port>`.

## Security Considerations

Binding to a Tailscale IP is different from binding to a LAN interface. It makes the service reachable to allowed tailnet devices, not arbitrary local network devices.

Still, this should be explicit configuration because development services may expose unauthenticated actions, debug data, or mutation endpoints.

Recommended guardrails:

- Default to `localhost`.
- Log the effective listener address on startup and deploy.
- Make `0.0.0.0` explicit and visible in status output.
- Prefer documentation examples that bind to a Tailscale IP instead of all interfaces.

## Acceptance Criteria

- Existing Anito deployments continue to listen on `localhost:<stable_port>` by default.
- A configured bind address causes the stable proxy to listen on `<bind_address>:<stable_port>`.
- Managed services still receive only `PORT=<ephemeral_port>` and can continue listening on localhost.
- Health checks and proxy swaps continue to use localhost internal ports.
- Status/API/dashboard output exposes the effective address.
- Tests cover default localhost binding and explicit bind address binding.

## Short-Term Workaround

Until this exists, use SSH local forwarding from the client machine:

```bash
ssh -N -L 5174:127.0.0.1:5174 johns-macbook-pro
```

Then open:

```text
http://localhost:5174
```

This is lower-risk for immediate access, but less convenient for daily use.
