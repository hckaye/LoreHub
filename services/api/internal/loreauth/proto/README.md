# Lore UCS auth contract

`auth_api.proto` is the `epic_urc.UrcAuthApi` contract shipped by Lore
v0.8.6. It was copied from the read-only source tree at
`/tmp/lorehub-lore-source.dy4rS8` at release commit
`4fa9870b86efc17a54cb7ead7b0c0364b36e8348`.

The Go bindings beside this directory are generated bindings, not a new
protocol. Regenerate them with `go generate ./internal/loreauth` using:

- `protoc` 28 or newer;
- `protoc-gen-go` v1.36.6;
- `protoc-gen-go-grpc` v1.5.1.

`generate.sh` applies a deterministic layout pass after generation. It only
wraps generated expressions and removes redundant reflection JSON tags; the
protobuf field numbers, names, service paths, and wire types remain unchanged.

The generated package path is deliberately `epic_urc`. Do not rename fields,
change tags, add wildcard resources, or add a second authentication protocol.
When upgrading Lore, compare the upstream `lore-proto/proto/auth_api.proto`,
the UCS client, and the server JWT verifier before changing this vendored
contract.

`rebac_api.proto` is also vendored from the same Lore 0.8.6 release. Lore's
repository create/delete RPCs call this companion `ucs.auth.RebacApi` on the
same TLS endpoint as `epic_urc.UrcAuthApi`; it uses the same canonical
`urc-{32 hex}` resource IDs and the same PostgreSQL authorization boundary.
