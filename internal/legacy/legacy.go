// Package legacy is init's migration machinery: it collapses stamped canonical
// blocks in a handwritten markdown artifact into `{{> name}}` directives,
// self-verifies the result renders back to the original, and (unless dry-run)
// writes X.src.md plus a freshly-rendered X.md.
package legacy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yasyf/cc-guides/guide"
)

// Sentinel errors. ErrDrift maps to CLI exit 1 (a fixable mismatch); everything
// else denotes invalid input (exit 2).
var (
	ErrNotMarkdown     = errors.New("init is markdown-only")
	ErrAlreadyBannered = errors.New("already migrated: input carries a cc-guides banner")
	ErrSourceExists    = errors.New("refusing to clobber an existing .src file")
	ErrDrift           = errors.New("migration blocked: mismatched or unknown fragment blocks")
	ErrCollision       = errors.New("a remaining literal line parses as a directive")
	ErrSelfVerify      = errors.New("self-verify failed: the migrated source does not re-render to the original")
)

// Status is a migration row status.
type Status string

// Migration row statuses reported on stdout.
const (
	StatusVerified Status = "VERIFIED"
	StatusMismatch Status = "MISMATCH"
	StatusUnknown  Status = "UNKNOWN"
)

// Row is one machine-readable migration outcome line.
type Row struct {
	Status Status
	Detail string
}

// Options configures a migration.
type Options struct {
	DryRun         bool
	KeepMismatched bool
	Version        string // banner version stamped into the re-rendered artifact
	Resolver       guide.Resolver
}

// Result is a migration outcome.
type Result struct {
	Rows       []Row
	SourcePath string
	Artifact   string
	Wrote      bool
}

var (
	beginStampRe = regexp.MustCompile(`^<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/([a-z0-9][a-z0-9-]*)\.md@(?:[0-9a-f]{40}|pending) -->\s*$`)
	endStampRe   = regexp.MustCompile(`^<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/([a-z0-9][a-z0-9-]*)\.md -->\s*$`)
)

type item struct {
	directive bool
	name      string // when directive
	text      string // when literal
}

// Migrate runs init over a single markdown artifact.
func Migrate(artifact string, opts Options) (Result, error) {
	res := Result{Artifact: artifact}

	kind, err := guide.KindForPath(artifact)
	if err != nil || kind != guide.KindMD {
		return res, fmt.Errorf("%w: %q", ErrNotMarkdown, artifact)
	}
	raw, err := os.ReadFile(artifact) // #nosec G304 -- init reads the user-named artifact by design
	if err != nil {
		return res, err
	}
	if _, ok := guide.ParseBanner(guide.KindMD, raw); ok {
		return res, fmt.Errorf("%w: %q", ErrAlreadyBannered, artifact)
	}

	res.SourcePath = guide.SourcePath(artifact)
	if _, err := os.Stat(res.SourcePath); err == nil {
		return res, fmt.Errorf("%w: %q", ErrSourceExists, res.SourcePath)
	}

	// CRLF is a hard error; guide.Parse rejects it too, but check early for a
	// clear message before any scanning.
	if strings.IndexByte(string(raw), '\r') >= 0 {
		return res, guide.ErrCRLF
	}

	lines := strings.Split(string(raw), "\n")
	items, rows, skipRecon, bad := scan(lines, opts.Resolver)
	res.Rows = rows

	if bad && !opts.KeepMismatched {
		return res, ErrDrift
	}

	// Collision scan: any surviving literal line that is directive-shaped.
	for _, it := range items {
		if !it.directive && guide.LooksLikeDirective(it.text) {
			return res, fmt.Errorf("%w: %q", ErrCollision, it.text)
		}
	}

	candidate := buildSource(items)

	// Self-verify: the candidate must re-render to the original modulo the banner,
	// the deleted stamp/end lines, and per-line trailing whitespace.
	doc, err := guide.Parse(candidate, guide.KindMD)
	if err != nil {
		return res, err
	}
	renderedBody, err := guide.Render(doc, opts.Resolver)
	if err != nil {
		return res, err
	}
	reconstruction := reconstruct(lines, skipRecon)
	if !equalModuloTrailingWS(reconstruction, string(renderedBody)) {
		return res, ErrSelfVerify
	}

	final := guide.AddBanner(guide.KindMD, opts.Version, filepath.Base(res.SourcePath), renderedBody)

	res.Rows = append(res.Rows, Row{StatusVerified, artifact})
	if opts.DryRun {
		return res, nil
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(artifact); err == nil {
		mode = info.Mode().Perm()
	}
	// The migrated source and artifact are world-readable by design.
	if err := os.WriteFile(res.SourcePath, candidate, 0o644); err != nil { // #nosec G306 -- world-readable by design
		return res, err
	}
	if err := os.WriteFile(artifact, final, mode); err != nil { // #nosec G306 -- preserves the artifact's existing mode
		return res, err
	}
	res.Wrote = true
	return res, nil
}

// scan walks the original lines, collapsing matched stamped blocks into directive
// items and recording rows for unknown/mismatched blocks. skipRecon marks the
// original line indices (collapsed stamps and end markers) deleted for the
// self-verify reconstruction.
func scan(lines []string, resolver guide.Resolver) (items []item, rows []Row, skipRecon map[int]bool, bad bool) {
	skipRecon = map[int]bool{}
	n := len(lines)
	i := 0
	for i < n {
		m := beginStampRe.FindStringSubmatch(lines[i])
		if m == nil {
			items = append(items, item{text: lines[i]})
			i++
			continue
		}
		name := m[1]
		endIdx := findEnd(lines, i+1, name)
		frag, ok, _ := resolver.Resolve(name, guide.KindMD)
		if !ok {
			rows = append(rows, Row{StatusUnknown, name})
			bad = true
			i = keepLiteral(&items, lines, i, endIdx)
			continue
		}
		eb := strings.Split(strings.TrimSuffix(string(frag.Body), "\n"), "\n")

		var window []string
		var blockEnd int // last line index of the block (inclusive)
		if endIdx >= 0 {
			window = lines[i+1 : endIdx]
			blockEnd = endIdx
		} else {
			end := i + 1 + len(eb)
			if end > n {
				end = n
			}
			window = lines[i+1 : end]
			blockEnd = end - 1
		}

		if matchBody(window, eb) {
			items = append(items, item{directive: true, name: name})
			skipRecon[i] = true // stamp line
			if endIdx >= 0 {
				skipRecon[endIdx] = true // end marker line
			}
			i = blockEnd + 1
		} else {
			rows = append(rows, Row{StatusMismatch, name})
			bad = true
			if endIdx >= 0 {
				// End-marked block is self-delimiting; keep it verbatim.
				i = keepLiteral(&items, lines, i, endIdx)
			} else {
				// No end marker: the fixed-length window is only a guess at the
				// block's extent. Keep literal up to the next recognized
				// begin-stamp (or EOF) and resume there, so a valid stamp inside
				// the guessed window is not swallowed as literal.
				i = keepLiteralUpTo(&items, lines, i, nextBeginStamp(lines, i+1))
			}
		}
	}
	return items, rows, skipRecon, bad
}

// keepLiteral copies a block verbatim into items and returns the next index. The
// block spans [start, blockEnd]; blockEnd defaults to endIdx (an end-marked
// block) or, absent an end marker, just the stamp line.
func keepLiteral(items *[]item, lines []string, start, endIdx int, blockEndOpt ...int) int {
	end := start
	switch {
	case len(blockEndOpt) > 0:
		end = blockEndOpt[0]
	case endIdx >= 0:
		end = endIdx
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	for k := start; k <= end; k++ {
		*items = append(*items, item{text: lines[k]})
	}
	return end + 1
}

// nextBeginStamp returns the index of the next recognized begin-stamp at or
// after from, or len(lines) when there is none.
func nextBeginStamp(lines []string, from int) int {
	for j := from; j < len(lines); j++ {
		if beginStampRe.MatchString(lines[j]) {
			return j
		}
	}
	return len(lines)
}

// keepLiteralUpTo copies lines [start, stop) verbatim into items and returns
// stop, the index to resume scanning from.
func keepLiteralUpTo(items *[]item, lines []string, start, stop int) int {
	for k := start; k < stop; k++ {
		*items = append(*items, item{text: lines[k]})
	}
	return stop
}

func findEnd(lines []string, from int, name string) int {
	for j := from; j < len(lines); j++ {
		if em := endStampRe.FindStringSubmatch(lines[j]); em != nil && em[1] == name {
			return j
		}
	}
	return -1
}

func matchBody(window, embedded []string) bool {
	if len(window) != len(embedded) {
		return false
	}
	for i := range window {
		if strings.TrimRight(window[i], " \t") != strings.TrimRight(embedded[i], " \t") {
			return false
		}
	}
	return true
}

func buildSource(items []item) []byte {
	var sb strings.Builder
	for idx, it := range items {
		if it.directive {
			sb.WriteString("{{> " + it.name + "}}")
		} else {
			sb.WriteString(it.text)
		}
		if idx < len(items)-1 {
			sb.WriteByte('\n')
		}
	}
	return guide.EnsureSingleTrailingNewline([]byte(sb.String()))
}

func reconstruct(lines []string, skip map[int]bool) string {
	var kept []string
	for i, l := range lines {
		if skip[i] {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

func equalModuloTrailingWS(a, b string) bool {
	la, lb := normLines(a), normLines(b)
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if la[i] != lb[i] {
			return false
		}
	}
	return true
}

func normLines(text string) []string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimRight(lines[len(lines)-1], " \t") == "" {
		lines = lines[:len(lines)-1]
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return lines
}
