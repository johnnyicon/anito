# Gomanan Authority Client Identity Closeout

AWF plan: `019f2cdf-5b18-7dcf-bddf-326c07c21a23`
AWF mission: `019f2cdf-8fe8-731c-bcde-227bf401389d`
AWF brief: `019f2cdf-c8d3-78c5-bb80-258ebbe3b707`

## Anito Header Contract

Anito now forwards the proxy-owned caller IP in:

```text
X-Anito-Client-IP
```

The value is derived from the inbound socket peer address, `r.RemoteAddr`, normalized to IP only. Anito overwrites inbound client-supplied `X-Anito-Client-IP` values before forwarding to the upstream service, so callers cannot spoof this identity header through the proxy.

Existing `X-Anito-Proxy: 1` response behavior remains intact.

## Tests Run

```text
go test ./internal/proxy -count=1
ok  	github.com/johnnyicon/anito/internal/proxy	0.677s

go test ./...
ok  	github.com/johnnyicon/anito/internal/proxy	0.606s
ok  	github.com/johnnyicon/anito/internal/server	2.065s
ok  	github.com/johnnyicon/anito/internal/service	3.411s
```

All tests passed.

## Gomanan Authority Configuration

Configure Gomanan Authority to trust Anito's proxy-owned identity header:

```text
GOMANAN_AUTH_TRUSTED_FORWARD_HEADER=X-Anito-Client-IP
```

Gomanan should only trust this header when the immediate peer is loopback, which matches the Anito deployment path: tailnet caller to Anito stable port, then Anito to `gomanan-daemon` over loopback.

## Remaining Gomanan Hard Cutover Verification

Perform these checks from the Gomanan Authority side after deploying this Anito change and setting the environment variable above:

1. A tailnet caller routed through `100.127.96.57:8100` is classified as `class=tailnet`, not `class=loopback`.
2. Under hard enforcement, a non-allowlisted tailnet caller receives `403`.
3. Existing allowlisted tailnet clients continue to reach Gomanan Authority through Anito.

This Anito thread did not implement allowlist authorization, Tailscale WhoIs, or the Gomanan enforcement cutover.
