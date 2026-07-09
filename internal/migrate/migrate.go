// Package migrate converts a v1 source (root X.src.md with {{> name}} directives)
// into the v3 shape: a .claude/fragments/<target>/ artifact dir holding repo-local
// `*.fragment.*` prose pieces and a layout.toml that composes them alongside
// imports of shared fragments. It is the shared engine behind both the `migrate`
// CLI command (which parses a real .src) and `init` (which synthesizes one from a
// legacy stamped artifact); both feed Build a segment list and the authoritative
// body the composition must reproduce.
package migrate

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/layout"
	"github.com/yasyf/cc-guides/source"
)

// atxHeadingRe matches an ATX markdown heading, capturing its text.
var atxHeadingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)

// slugMaxLen caps a generated fragment slug so a long heading does not become an
// unwieldy filename.
const slugMaxLen = 40

// Segment is one piece of a v1 source: a local prose run (edge-trimmed,
// non-empty) or an import of a shared fragment.
type Segment struct {
	Prose  string        // local prose when Import == nil
	Import *layout.Entry // import entry otherwise
}

// Segments splits a parsed v1 doc into ordered prose/import segments. Consecutive
// literal lines form one prose run (leading/blank edges trimmed — the composition
// joiner re-adds the single blank line between pieces); each directive becomes a
// cc-skills import. A run that trims to empty (only blank lines between two
// directives) is dropped.
func Segments(doc *guide.Doc) []Segment {
	var segs []Segment
	var run []string
	flush := func() {
		text := edgeTrim(run)
		run = nil
		if text != "" {
			segs = append(segs, Segment{Prose: text})
		}
	}
	for _, n := range doc.Nodes {
		if n.Include != nil {
			flush()
			segs = append(segs, Segment{Import: includeToEntry(n.Include)})
			continue
		}
		run = append(run, n.Literal)
	}
	flush()
	return segs
}

// includeToEntry maps a v1 `{{> name k=v}}` directive to a cc-skills import entry.
func includeToEntry(inc *guide.Include) *layout.Entry {
	e := &layout.Entry{Alias: layout.DefaultAlias, Name: inc.Name}
	if len(inc.Args) > 0 {
		e.Args = map[string]string{}
		for k, v := range inc.Args {
			e.Args[k] = v
			e.Keys = append(e.Keys, k)
		}
		sort.Strings(e.Keys)
	}
	return e
}

// Input drives Build.
type Input struct {
	Target     string            // artifact target relpath, e.g. "AGENTS.md"
	Kind       guide.Kind        // artifact kind (from the target extension)
	Segments   []Segment         // ordered prose/import segments
	ExpectBody []byte            // the body-after-banner the composition must reproduce
	Tolerant   bool              // trailing-whitespace-tolerant compare (init) vs strict byte-compare (migrate)
	Version    string            // banner version
	Importer   source.Importer   // resolves shared imports for the compose + self-verify
	Overrides  map[string][]byte // directive name -> flat v1 override body, folded into local fragments
}

// Output is a computed migration; the CLI performs every filesystem write.
type Output struct {
	LayoutDir     string            // repo-relative, e.g. ".claude/fragments/AGENTS.md"
	LayoutTOML    []byte            // layout.toml contents
	FragmentFiles map[string][]byte // basename (<slug>.fragment.<ext>) -> body
	Artifact      []byte            // rendered target artifact, banner included
}

// Build assigns slugs, composes, self-verifies against ExpectBody, and returns the
// computed artifact dir + rendered artifact. A self-verify mismatch is an error
// carrying a unified diff; the CLI then writes nothing.
func Build(ctx context.Context, in Input) (Output, error) {
	entries, files, err := assign(in.Segments, in.Kind, in.Overrides)
	if err != nil {
		return Output{}, err
	}
	lay := &layout.Layout{
		Sources: map[string]string{layout.DefaultAlias: layout.DefaultSourceSpec},
		Entries: entries,
	}

	pieces, err := resolvePieces(ctx, entries, files, in.Kind, in.Importer)
	if err != nil {
		return Output{}, err
	}
	body, err := guide.Compose(in.Kind, pieces)
	if err != nil {
		return Output{}, err
	}

	// A folded v1 override was rendered in the deployed artifact wrapped in v1
	// `local:` markers, which v3 composition does not emit; strip those marker lines
	// from the expected body so the byte-compare reflects only content.
	expect := in.ExpectBody
	if len(in.Overrides) > 0 {
		expect = stripLocalMarkers(expect)
	}
	if mismatch := compare(expect, body, in.Tolerant); mismatch {
		return Output{}, &SelfVerifyError{Diff: guide.UnifiedDiff(in.Target, expect, body)}
	}

	dir := guide.FragmentsRoot + "/" + in.Target
	fragmentsStr := pinString(lay, in.Importer)
	artifact := guide.AddBanner(in.Kind, in.Version, dir, fragmentsStr, body)

	return Output{
		LayoutDir:     dir,
		LayoutTOML:    layout.Encode(lay),
		FragmentFiles: files,
		Artifact:      artifact,
	}, nil
}

// SelfVerifyError is returned when the composed body does not reproduce the
// authoritative body. It carries a diff for the operator.
type SelfVerifyError struct {
	Diff string
}

func (e *SelfVerifyError) Error() string {
	return "self-verify failed: the composed artifact does not reproduce the original body"
}

// assign turns segments into layout entries plus the fragment files for local
// pieces. Prose slugs are the kebab of the run's first ATX heading (capped), else
// part-<n>, deduped with -2/-3 suffixes; there are no numeric prefixes — ordering
// is layout.toml's job. An import whose directive name has a flat v1 override in
// overrides is folded into a LOCAL fragment named for the directive, preserving
// the repo's override instead of reverting it to the shared import.
func assign(segs []Segment, kind guide.Kind, overrides map[string][]byte) ([]layout.Entry, map[string][]byte, error) {
	entries := make([]layout.Entry, 0, len(segs))
	files := map[string][]byte{}
	used := map[string]int{}
	part := 0
	for _, s := range segs {
		if s.Import != nil {
			if ov, ok := overrides[s.Import.Name]; ok {
				slug := dedupe(s.Import.Name, used)
				entries = append(entries, layout.Entry{Name: slug})
				files[slug+".fragment"+kind.Ext()] = guide.EnsureSingleTrailingNewline([]byte(edgeTrim(strings.Split(string(ov), "\n"))))
				continue
			}
			entries = append(entries, *s.Import)
			continue
		}
		base := slugFromProse(s.Prose)
		if base == "" {
			part++
			base = fmt.Sprintf("part-%d", part)
		}
		slug := dedupe(base, used)
		entries = append(entries, layout.Entry{Name: slug})
		files[slug+".fragment"+kind.Ext()] = guide.EnsureSingleTrailingNewline([]byte(s.Prose))
	}
	return entries, files, nil
}

// localMarkerRe matches a v1 `local:` provenance marker line (either comment
// style), which a folded override no longer carries in v3.
var localMarkerRe = regexp.MustCompile(`^(?:<!-- /?local: .* -->|# /?local: .*)$`)

// stripLocalMarkers removes v1 `local:` marker lines from a body so a folded
// override's marker-free composition byte-matches the deployed artifact's content.
func stripLocalMarkers(body []byte) []byte {
	lines := strings.Split(string(body), "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if localMarkerRe.MatchString(l) {
			continue
		}
		kept = append(kept, l)
	}
	return []byte(strings.Join(kept, "\n"))
}

// resolvePieces turns entries into composed pieces: prose from the just-built
// fragment files, imports from the Importer (kind-mismatch and unknown-name
// diagnostics deferred to the caller via a clear error).
func resolvePieces(ctx context.Context, entries []layout.Entry, files map[string][]byte, kind guide.Kind, imp source.Importer) ([]guide.Piece, error) {
	pieces := make([]guide.Piece, 0, len(entries))
	for _, e := range entries {
		if !e.IsImport() {
			pieces = append(pieces, guide.Piece{
				Body:   files[e.Name+".fragment"+kind.Ext()],
				Origin: e.Name + ".fragment" + kind.Ext(),
			})
			continue
		}
		body, found, err := imp.Resolve(ctx, e.Alias, e.Name, kind)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: %s (kind %s)", guide.ErrUnknownFragment, e.Ref(), kind)
		}
		pin, _ := imp.Pin(e.Alias)
		var args map[string]string
		if len(e.Args) > 0 {
			args = e.Args
		}
		pieces = append(pieces, guide.Piece{
			Body:   body,
			Args:   args,
			Keys:   e.Keys,
			Origin: e.Ref() + "@" + pin,
		})
	}
	return pieces, nil
}

// pinString builds the banner's `fragments=` value: alias-sorted `alias@sha12`
// pins, `none` for zero imports, or `local` when any used alias resolved from a
// local source.
func pinString(lay *layout.Layout, imp source.Importer) string {
	aliases := lay.UsedAliases()
	if len(aliases) == 0 {
		return "none"
	}
	pins := make([]string, 0, len(aliases))
	for _, a := range aliases {
		pin, ok := imp.Pin(a)
		if !ok || pin == source.LocalPin {
			return source.LocalPin
		}
		pins = append(pins, a+"@"+pin)
	}
	return strings.Join(pins, ",")
}

func slugFromProse(prose string) string {
	for _, line := range strings.Split(prose, "\n") {
		m := atxHeadingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return slugify(m[1])
	}
	return ""
}

// slugify lowercases text, collapses every run of non-[a-z0-9] into a single
// hyphen, trims hyphens, and caps the length — returning "" when nothing valid
// remains so the caller falls back to part-<n>.
func slugify(text string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > slugMaxLen {
		s = strings.Trim(s[:slugMaxLen], "-")
	}
	if !guide.ValidName(s) {
		return ""
	}
	return s
}

func dedupe(base string, used map[string]int) string {
	used[base]++
	if n := used[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

// edgeTrim joins lines and strips leading and trailing blank lines.
func edgeTrim(lines []string) string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// compare reports whether want and got differ, tolerating per-line trailing
// whitespace and trailing blank lines when tolerant.
func compare(want, got []byte, tolerant bool) (mismatch bool) {
	if !tolerant {
		return string(want) != string(got)
	}
	return !equalModuloTrailingWS(string(want), string(got))
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
