# Release smoke fixture

A tiny, self-contained shared-fragments pack — one fragment per kind (`sh`, `yml`,
`json`, `toml`). The release workflow's per-platform smoke job downloads each
published binary and runs `cc-guides lint .github/smoke` against it, which exercises
every cgo tree-sitter grammar and semantic decoder linked into the binary. It is the
tripwire for silent cgo rot on the darwin leg (built natively; the linux legs
cross-compile with zig). `cc-guides lint` skips this README (a pack-root `README.md`
is documentation, not a fragment).
