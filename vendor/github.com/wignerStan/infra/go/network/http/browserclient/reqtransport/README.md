# Native browser-profile HTTP transport

`reqtransport` is the in-process Go backend for browser-profile HTTP. It binds
`imroc/req` and uTLS; the existing parent `browserclient` worker backend is
unchanged. No browser, native helper process, credentials, environment-selected
proxy, or application session is acquired by this adapter.

```go
transport, err := reqtransport.New(reqtransport.Options{Profile: "chrome"})
if err != nil { return err }
defer transport.CloseIdleConnections()
jar, err := cookiejar.New(nil)
if err != nil { return err }
client := &http.Client{
    Transport: transport,
    Jar: jar,
    Timeout: 15 * time.Second,
    CheckRedirect: func(*http.Request, []*http.Request) error {
        return http.ErrUseLastResponse
    },
}
```

Profiles are `chrome` (120, default), `firefox` (120), and `safari` (16.6),
as bundled by pinned `github.com/imroc/req/v3 v3.61.0`. They configure the TLS
ClientHello, HTTP/2 settings and browser header defaults. These are named
bundled profiles, not a guarantee of current browser parity or access to an
anti-bot-protected site. Explicit request headers override defaults. The adapter
clones headers before passing requests to the backend.

The caller owns the standard `http.Client`: CookieJar persistence, redirect
policy, credentials, request contexts, overall timeout, and response-body
consumption. `RoundTrip` streams bodies, never follows redirects or adds
application retries. Closing idle connections does not cancel active requests.

Certificate verification is enabled by default. `RootCAs` is cloned on
construction. `InsecureSkipVerify` is an explicit opt-out for a caller-managed
self-signed endpoint; it must not be mistaken for certificate pinning. Empty
`ProxyURL` means a direct connection, regardless of environment variables.
`Dialer` injects the caller-owned connection path. `VerifyConnection` runs on
every native TLS handshake before HTTP or upgraded-stream bytes, so consumers
can retain a stricter CA/pinning policy. A callback failure closes the connection.

Explicit HTTP(S) and SOCKS5(H) proxy URLs are supported by HTTP exchanges.
Invalid profiles and unsupported proxy URLs are rejected before network effects.

`DialTLS(ctx, "host:port")` returns a caller-owned raw connection and negotiated
TLS state using the same browser handshake and certificate policy. This enables
consumer-owned HTTP upgrades whose success uses 200 rather than 101. The caller
must honor ALPN and close the stream. Raw TLS with an explicit proxy is rejected
before dialing; it never silently bypasses that proxy. Native connection
establishment and its cancellation ownership use infra's existing `network/dial`
primitive.

## Acceptance and evidence

Run from the containing standalone module:

```sh
GOWORK=off go test -race ./...
```

Local native TLS fixtures verify actual ClientHello GREASE on the Chrome/Safari
profiles, profile headers, trusted/untrusted certificates, the explicit TLS
opt-out, caller CookieJar persistence, no redirect following, unchanged request
headers, cancelled requests, raw TLS exchanges, and rejection of a configured
proxy on the raw-stream API. They do not use external credentials or OAuth.

This addition is based on PR #185's `repo/migrate/fcis-register-schema-v11`
branch. The adapter contains no gateway addresses, account data, VPN policy,
TUN devices, or operating-system route changes.
