# H3C OpenConnect module patches

The normal Go module pin in `source.json` is the upstream authority. Its Go
checksum is verified before applying this package. No additional submodule or
source checkout is used. `apply_order.txt` records each semantic patch and its
SHA-256; every patch owns one exact target. `run.py` verifies the reconstructed
module digest and refuses to replace a materialization containing unrecorded
edits. The existing vendor generator projects the patched module into Go vendor.

```sh
python3 patches/sing-openconnect/run.py prepare
# Compile and execute tests/builds through .github/workflows/openconnect-h3c.yml
# CI invokes: python3 patches/sing-openconnect/run.py test
python3 tools/vendor/materialize.py --keep-work
python3 patches/sing-openconnect/run.py check --vendor
python3 tools/vendor/verify.py
```

The H3C flavor uses password authentication or an existing `svpnginfo` cookie,
then upgrades TLS with `NET_EXTEND` and exchanges framed IPv4 packets through
sing-openconnect. It uses the reusable infra native browser transport, pinned
as a regular Go module. Authentication and application policy stay in this
package. Explicit CA/pin policy and the sing-box outbound dialer are retained
for both control and data connections. Unsupported CAPTCHA/MFA, tokens, client
certificates and custom TLS version bounds fail before authentication.

Enable `with_openconnect,with_gvisor,with_utls` when building sing-box. Select
`flavor: h3c` and `system: false` on an OpenConnect endpoint for user-space
networking. Direct TCP inbounds or a SOCKS inbound can route through it. Shared
account credentials and gateway-specific targets belong in a private runtime
config, never in this patch package.

Local native TLS fixtures cover XML credential escaping, cookie propagation,
packet framing, tunnel configuration, packet roundtrip and one-shot logout.
Live acceptance on Incus obtained OpenSSH banners from both configured GPU
servers through the separately built sing-box. Server login and computation
were not performed. No kernel TUN device, host route change or daemon replacement
is required. The test gateway currently uses an explicit insecure TLS setting
because its self-signed certificate has no SAN; that is not certificate pinning.

Builds are owned by the `H3C OpenConnect` GitHub Actions workflow. Download its
revision-labelled artifact and verify `SHA256SUMS` and `SOURCE_COMMIT` before
running it on Incus. This workflow does not publish a release.
