# cc-guides Development Guide

Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and shell artifacts from embedded, versioned fragments Distributed via Homebrew: `brew install yasyf/tap/cc-guides`.

## Repository Structure

```
cc-guides/
├── cmd/cc-guides/   # main package — the CLI entry point
├── internal/
│   ├── cli/               # cobra command tree — TODO(bootstrap): name the commands
│   ├── version/           # build version, stamped via -ldflags
│   └── log/               # slog setup
├── .github/               # GitHub Actions workflows
├── AGENTS.md              # This file — shared conventions
└── README.md              # Project overview
```

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/ask-before-assuming.md@c18a88db2aee0f60b0da4a76a74a996813aa890f -->
## Ask Before Assuming

When the user's request has ambiguity — unclear scope, multiple plausible interpretations, undefined edge cases, or unspecified tradeoffs — stop and ask. Propose 2-4 concrete options and let the user pick, or list the assumptions you'd otherwise make and ask which ones hold. There is no such thing as too many questions; one wrong implementation costs more than ten clarifying exchanges. Default to interrogating the user when in doubt — multiple short questions early beat a wrong direction later.
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/ask-before-assuming.md -->

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/code-review-response.md@c18a88db2aee0f60b0da4a76a74a996813aa890f -->
## Code Review Response (Plan Re-Entry)

When the user reviews code you wrote and re-enters plan mode — whether by leaving inline diff comments, pasting a numbered list of issues, or otherwise sending review-shaped feedback after a recent edit cycle — you MUST:

0. **Delegate context-gathering to a subagent.** Spawn one `Explore` subagent with every cite (file:line + the user's verbatim comment text). Instruct it to, per cite, `Grep` the file with ~5 lines of context either side of the cited line (`-B 5 -A 5`), and only escalate to a full `Read` when the ±5-line window is insufficient (e.g. the comment refers to a function defined further up). Have it also surface sibling call sites with the same issue (Grep across the module). Use the subagent's digest as your source of truth when drafting the plan. Do NOT bulk-`Read` the cited files yourself in the main turn — it bloats the main context window before you've even started writing the plan.
1. **Draft a new plan**, not a code change. Plan-mode re-entry is the user asking "let's align on what you'll do next," not "go fix it."
2. **Inline every comment verbatim** in the plan. Each comment gets a short anchor (`#N`, the file:line if provided, or a quoted excerpt) plus the user's exact wording in a blockquote or `*"…"*` italics. Do not paraphrase. The user must be able to scan the plan and see every comment they wrote reproduced exactly.
3. **Cluster when many.** If there are more than ~5 comments, group them into themes (e.g. "T1 — Guards against impossible states") and list every verbatim trigger per theme. Address every cited line *and* extrapolate the rule to other call sites that have the same problem.
4. **Map every comment.** Maintain a "verbatim feedback table" near the end of the plan with one row per comment: `# | file:line | verbatim | cluster`. No comment may be silently dropped.
5. **Do NOT start implementing** before the plan is approved via `ExitPlanMode`. Delegating reads via #0 is fine; editing source is not.

The canonical shape is the `Overarching themes` table + per-cluster `**#N (verbatim):** *"…"*` anchors + final mapping table. When a comment is ambiguous, ask via `AskUserQuestion` rather than guessing.

### Plan follow-up questions

After you write a plan, the user may respond with questions ("why this approach?", "what about X?", "did you consider Y?") rather than approval. In that case you MUST NOT edit the plan to bake in answers. Instead:

1. **Answer the question conversationally** in your text response — explain the reasoning, the tradeoffs, and what you'd recommend.
2. **Propose options via `AskUserQuestion`** — one question per ambiguity, each with 2–4 concrete options the user can pick from. Batch related questions into one `AskUserQuestion` call.
3. **Wait for the user's choice** before editing the plan. The plan edit then reflects the user's pick, not your assumption.

Editing the plan first robs the user of the choice and forces them to diff the plan to find what you decided. Surface the decision point first.
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/code-review-response.md -->

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/parallelize.md@c18a88db2aee0f60b0da4a76a74a996813aa890f -->
## Parallelize Independent Work

Sequential is the exception, not the default. Two steps that don't consume each other's output run at the same time; when unsure whether they're independent, assume they are and fan out. The orchestrator routes and synthesizes — it never executes work a subagent could. Pick the surface by scale:

- **Batch tool calls in one message** — the cheapest parallelism and the most missed. Independent reads, greps, globs, and read-only Bash go in a *single* message, never one per turn.
- **Parallel subagent calls in one message** — ad-hoc independent investigations: "explore X while I check Y", multi-file reviews, independent edits. One message, N `Agent` tool uses, results gathered in parallel.
- **Dynamic workflow** — default for substantive multi-step work; the script holds the loop, branching, and intermediate results. See CLAUDE.md `## Plan Execution & Orchestration`.
- **Named team** — long-running peers needing agent-to-agent handoffs mid-run, via `TeamCreate`.

Single-step exception: one task, no parallel sibling, no follow-on → one subagent call is fine.
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/parallelize.md -->

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/writing-plans.md@c18a88db2aee0f60b0da4a76a74a996813aa890f -->
## Writing Plans

When you write a plan — in plan mode, or any "here's what I'll do" before you start editing — use this shape so it's fast to scan and complete enough to execute:

- **Context** — why this change: the problem or need, what prompted it, the intended outcome.
- **Approach** — the recommended approach only (not every alternative you weighed), as ordered steps. Name the critical files to touch; for a pattern repeated across many files, describe it once with a few representative paths instead of listing them all. Cite existing utilities/patterns you'll reuse, with their paths.
- **Potential Pitfalls** — the sharp edges specific to this work: ordering constraints, code that looks safe to change but isn't, prior art that must not be "fixed", state that diverges from how it's described. One bullet each — front-load the gotchas you'd otherwise hit mid-implementation.
- **Workflow Plan** — required in every plan; a plan without it is incomplete. One line on what the main agent alone does (track state, dispatch, decide, report), then a `Phase | Shape | Agents | Verification` table covering every fan-out the plan anticipates: Shape is `pipeline` / `parallel` / `loop`; Agents names each phase's model and effort per the Models table (e.g. `opus xhigh ×4`, `sonnet low → codex`); Verification names the check that gates each phase's output. When nothing fans out, one line saying everything stays at the main-agent level replaces the table.
- **Verification** — how to prove it works end to end: the exact commands to run, tests to add, and behavior to observe.
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/writing-plans.md -->

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/ccx.md@509eb3c7061c36b7abee2813c8418b71670c9d98 -->
## Compact Context (ccx)

`cc-context` — the `ccx` CLI and the `cc-context` MCP (its `mcp__cc-context__*` tools mirror the query surface — read, search, symbol, outline, diff, edit — plus `ccx_exec`/`ccx_exec_tools` for multi-call composition and `BashFormat` for JSON re-encoding) — is the DEFAULT for reading code, finding symbols, searching, and reviewing diffs. It returns token-bounded output (signatures + line numbers, explicit overflow, never silent truncation) instead of raw dumps, and the capt-hook `ccx` guard pack BLOCKS the token-heavy primitives — so reach for ccx first.

1. **Orient a repo** → `ccx repo overview`
2. **"How does X work / where is Y" (intent)** → `ccx code search "<question>"` (semantic, semble-backed)
3. **A specific symbol (def + callers + callees)** → `ccx code symbol <name>` (alias `ccx code grok`)
4. **Literal / structural text** → `ccx code grep <text> [--glob G]`
5. **List files** → `ccx repo find "<glob>"`
6. **Read a file** → `ccx code outline <file-or-dir>` first (ast-grep structural map for the languages it outlines and any directory, tilth signatures otherwise), then `ccx code read <file> --section A-B` for the part you need (whole file: `ccx code read <file> --full`)
7. **Edit a file** → `ccx code edit <file> --at A-B#hash --content <text>` (hash-verified write: refuses on anchor mismatch, re-anchors moved content, returns the new anchor so edits chain; `--content -` reads stdin, `--delete` removes the range)
8. **Review changes** → `ccx vcs diff [src]` (structural, jj-aware; exact hunks: `git diff -- <file>`)
9. **Inspect one commit** → `ccx vcs show [ref]` (message + structural per-file diff; default `@-`/HEAD)
10. **How a file evolved** → `ccx vcs history <path> [-n N]` (per-commit sha · date · subject + changed symbols)
11. **Locate a repo/module/package on disk** → `ccx repo locate <name>` (sibling repo, Go module, or Python package; prints tab-separated `kind`/`path`/`version`, exit 3 when unresolved)
12. **Commit, push, watch CI** → `ccx vcs ship -m "<msg>"` (jj-aware commit + push + `gh run watch --exit-status` in one call)
13. **Compose several calls / post-process any output** → `ccx exec '<python>'` — a sandboxed script whose async host functions are every ccx query op, a gated `sh(cmd)`, and every stateless MCP server's tools (auto-reflected, no flag needed); only the script's return value enters context. Rule of thumb: one question → one ccx call (entries 1–12, 14–17); a pipeline, filter, fan-out, or any output you'd immediately post-process (project a JSON blob, sweep signatures across files, join search hits) → exec. Discover the host functions and the Python-subset rules with `ccx exec --list-tools` (MCP: `ccx_exec_tools`), once per session.
14. **Re-encode JSON tool output** → `ccx format -- <cmd>` (or `… | ccx format`) — a shape classifier picks the leanest encoding (prose, markdown table, CSV/TSV, TOON, TRON, JSONL, or compact JSON), never larger than compact JSON by bytes; `--format=X` forces one encoder
15. **Map a web page** → `ccx web outline <url>` (heading tree with stable `§` section refs; pages cache 24h, `--refresh` refetches)
16. **Read one section of a page** → `ccx web read <url> --section <ref>` (budget-capped section subtree + prev/next nav; whole page: `--full`)
17. **Ask a question of a page** → `ccx web search <url> "<question>"` (top-k relevant chunks with `<url> §2.3#hash` cites; hybrid BM25 + local embeddings, BM25-only with a note when `uv` is absent)

Entries 9–12 are CLI-only — the MCP mirrors the query surface (1–8) plus exec (13), format (14, as `BashFormat`), and web (15–17, as `ccx_web_outline`/`ccx_web_read`/`ccx_web_search`), not these.

Durable prose — plans, reviews, memory files — cites code as `path:line#hash` (e.g. `internal/render/finalize.go:31#k2fa`); any later session resolves the cite statelessly with ccx, because the hash re-anchors by content even after the file drifts.

Reach for your **LSP** when the answer must be exhaustive/structural (findReferences, rename, goToImplementation). Use **Grep/Glob** only for literal content in non-source files (logs, JSON, YAML).
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/ccx.md -->

## Go Style

Target Go 1.26+. Run `task build`, `task test` (`go test -race`), and `task lint`.

**Comments are terse and used sparingly — the code documents itself** through names, types, and organization. The one exception is documentation-generation comments: godoc on exported types, funcs, and the package, each starting with the identifier's name (`// NewRootCmd builds …`); unexported helpers get none. Beyond godoc, comment only for TODOs, non-obvious workarounds, or disabled code — never to restate the signature.

**Errors wrap with `%w`.** Return failures up the stack with `fmt.Errorf("…: %w", err)` and inspect them with `errors.Is` / `errors.As`, never string matching. See STYLEGUIDE.md § Error Handling.

**Structured logging via `log/slog`.** Diagnostics go through the configured default logger (`slog.Info`, `slog.Debug`) with key-value attrs — never `fmt.Println` for logging. See `internal/log`.

@STYLEGUIDE.md

## General Rules

**Minimal changes.** Stay within scope; fix the issue, then stop.

**Match surrounding code.** Follow the conventions of the file you're in, then the package.

**No defensive coding.** No fallbacks, shims, or backwards-compat layers; no guards against impossible states. If unused, delete it. Crash on the unexpected.

**Search before writing.** Before creating a helper, query the codebase via `ccx code search` (intent or symbol queries both work). Sibling packages win over re-implementation.

**Code stewardship.** When you touch a file, fix nearby bugs, style violations, and broken tests; don't wave them off as pre-existing or out of scope.

**Observe, don't infer.** Inspect actual data — read fixtures, dump structs, run the code — before reasoning from assumption.

**Don't use external failures as an excuse to stop.** API quota, rate-limit, and outage errors rarely block the whole task; trace the catch sites and confirm a failure actually stops you before claiming it does.

**Verify before asserting.** Don't report something as working, fixed, blocked, or impossible until you've checked — run it, read the output, reproduce the failure. "It should work" is not "it works."

**Reproduce before fixing.** When something breaks, isolate the smallest failing case before editing or re-running. Re-running the whole command while changing code between runs hides the root cause; narrow to the one failing test or input first.

**Research after repeated failure.** After ~2 failed approaches, stop guessing and gather evidence — search the web, read the docs and source — before a third attempt.

**Get a second opinion on a plateau.** On a debugging plateau (2 failed attempts before a 3rd), a non-trivial architectural decision, or algorithmic/security-sensitive code, get an outside check (e.g. `/codex`) before committing to the approach.

**Don't contort code to satisfy a linter.** The compiler and `golangci-lint` serve the code, not the other way around. Don't widen a type to `any`, bolt on a needless type assertion, or sprinkle `//nolint` just to silence a diagnostic. If a clean fix isn't obvious, leave the diagnostic — a visible one is preferable to scar tissue.

**Mechanical linting.** Running `gofumpt`/`golangci-lint` by hand is fine, and encouraged — the pre-commit hooks (prek: gofumpt + goimports + golangci-lint) also format and lint on every `git commit`; run `uvx prek install` once to activate them. Fix what needs human judgment and let the tooling own the mechanical churn. When reviewing code, don't flag mechanical lint violations (gofmt, import order, line length).

**Testing.** Tests live beside the code as `*_test.go`; run them with `task test` (`go test -race ./...`). Write table-driven tests with strict assertions against specific values, mock the boundaries your code talks to (network, filesystem, clock), and leave the code under test real.

**Writing docs.** When writing or revising docs, a README, a tutorial, a how-to, or reference, use the `writing-docs` skill (Diataxis modes, voice rules, and runnable code-sample rules) and run `slop-cop check <file> --lang=markdown` before you finish (slop-cop is a Go binary; if it's not on PATH, run the `/slop-cop-check` skill — never `uvx slop-cop`).

<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/version-control.md@c18a88db2aee0f60b0da4a76a74a996813aa890f -->
**Version control.** This repo is a colocated `jj` repo over git — prefer `jj` (`jj describe` / `jj commit`, `jj git push`) over raw `git` for day-to-day work. Commits stay atomic and scoped: one logical change each. For the routine commit, push, and watch-CI cycle, `ccx vcs ship -m "<msg>"` runs the whole dance in one call — a jj-aware commit, the push, and `gh run watch --exit-status` — instead of the three-to-six Bash calls it took by hand; drop to the manual `jj` steps when ship doesn't fit, like a multi-commit split or a partial-staging commit. A dirty tree is just the working-copy commit `@` — to land work on an updated remote, `jj git fetch` then `jj rebase` (your in-flight `@` rides along untouched); never `git stash` or a worktree + cherry-pick dance.

**Watch CI after every push.** A push that kicks off CI isn't done until the run is green. `ccx vcs ship` folds this in — it pushes, then runs `gh run watch --exit-status`, so a shipped commit is already watched to its conclusion. For a push ship didn't make, watch the run to completion yourself before you stop — `gh run watch "$(gh run list -L1 --json databaseId -q '.[0].databaseId')" --exit-status` — and never walk away from a red run: fix it or report it. (`--exit-status` exits non-zero when the run fails; give the run a moment to register before watching.)
<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/version-control.md -->

**Releases.** Tagging `v*` triggers `.github/workflows/release.yml`, which runs goreleaser to build the binaries, cut a GitHub release, and push the Homebrew cask to `yasyf/homebrew-tap`. The version comes from the tag. The release refuses to run unless the tagged commit is on `main` — tag a merged commit (e.g. `git tag vX.Y.Z origin/main`), not a feature branch. One-time setup: a `HOMEBREW_TAP_TOKEN` repo secret with push access to the tap. The macOS binaries are Developer-ID-signed and notarized when the `MACOS_*` repo secrets are set (optional; releases unsigned without them — see `reference/go-ci-and-release.md`).
