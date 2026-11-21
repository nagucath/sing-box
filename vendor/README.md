# Generated Go vendor projection

`vendor/` is a standard Go vendor projection generated from the main module
graph. DAE and Tailscale packages come from pristine `third_party/` pins after
the ordered parent-owned patch stacks under `patches/` are applied in the
ignored `.vendor-work/` generation area. The sing-openconnect Go module is
acquired through its pinned module checksum and patched by the package-owned
`patches/sing-openconnect/run.py` entrypoint, without another source checkout.

Do not edit files below `vendor/` directly. Regenerate and verify with:

```sh
python3 tools/vendor/materialize.py
python3 tools/vendor/verify.py --require-source
```

`vendor/modules.txt` is authoritative for Go's vendor-mode package mapping.
`deps/vendor-lock.json` records source commits, patch digests, patched Git tree
identities, the generated projection digest, and any repository-control files
that Go's projection copied and the materializer removed. No Git metadata,
gitlink, submodule, or nested repository is permitted below `vendor/`.
