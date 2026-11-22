# OpenConnect adapter patches

H3C protocol code and native TLS fixtures belong to the independently pinned
`github.com/wignerStan/infra/go/network/openconnect/h3c` package.
The infra module owns password login, browser TLS, NET_EXTEND, IPv4 framing,
and logout. The adapter maps upstream client lifecycle, cookies, packet buffers,
configuration, and errors to that public API. Other flavors remain upstream.

`source.json` pins the upstream module and its checksum. `apply_order.txt` is
the one package order and records each semantic patch and its SHA-256.
Each patch owns one exact target. `run.py` verifies the reconstructed digest
and refuses to replace unrecorded edits. The vendor generator projects the
adapter and its exact infra module dependency into ordinary Go vendor files.

GitHub Actions performs generation and validation. Infra’s OpenConnect library
workflow formats the adapter, resolves modules, refreshes package hashes, and
regenerates vendor. Download its generated patch for review and commit.
H3C OpenConnect then verifies the tracked source and builds a binary from
`git archive`, with module access disabled. No runtime gateway settings or
private credentials belong in this package.

The only remaining native TLS fixture here checks the upstream-to-infra adapter
through the public OpenConnect client. Protocol fixtures run in infra.
Enable `with_openconnect,with_gvisor,with_utls` and select `flavor: h3c`.
