package guide

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// BannerCoreRegex is the stable machine contract for the generated banner. It is
// pinned here and asserted verbatim in the tests; changing it is a migration.
//
// The optional `fragments=` group makes this ONE regex parse both banner
// generations: v1 (`… src=X | GENERATED …`, empty fragments group) and v2
// (`… src=X fragments=Y | GENERATED …`). `fragments=` sits AFTER `src=` so no
// ordering could make a v2 banner match a v1-only regex — there are no
// v1-compat contortions.
const BannerCoreRegex = `cc-guides (?P<version>\S+) src=(?P<src>\S+)(?: fragments=(?P<fragments>\S+))? \| GENERATED\b`

var bannerCoreRe = regexp.MustCompile(BannerCoreRegex)

// bannerCoreFmt renders the v2 banner core. The four %s are version, src,
// fragments, src.
const bannerCoreFmt = "cc-guides %s src=%s fragments=%s | GENERATED — do not edit: edit %s/ and run 'cc-guides render'. Everything below is in force."

// BannerCore returns the unwrapped v2 banner core string. fragments is the
// alias-sorted comma-joined pin list (`cc-skills@<sha12>[,…]`), or the sentinel
// `none` (zero imports) / `local` (a dev dir-render off a local source).
func BannerCore(version, src, fragments string) string {
	return fmt.Sprintf(bannerCoreFmt, version, src, fragments, src)
}

// bannerLine wraps the core in the comment style for kind.
func bannerLine(kind Kind, version, src, fragments string) string {
	core := BannerCore(version, src, fragments)
	switch kind {
	case KindMD:
		return "<!-- " + core + " -->"
	case KindSH:
		return "# " + core
	default:
		return core
	}
}

// AddBanner splices the v2 banner into composed content and returns bytes ending
// in exactly one newline. No blank line is inserted after the banner. For sh
// content beginning with `#!`, the shebang stays on line 1 and the banner becomes
// line 2.
//
// AddBanner is self-pinning-friendly: every field of the banner line is derived
// from (version, src, fragments), so `check` can reproduce a banner verbatim by
// passing the strings it parsed off the artifact's own banner.
func AddBanner(kind Kind, version, src, fragments string, content []byte) []byte {
	line := bannerLine(kind, version, src, fragments)
	body := string(content)
	var out string
	if kind == KindSH && strings.HasPrefix(body, "#!") {
		if nl := strings.IndexByte(body, '\n'); nl >= 0 {
			out = body[:nl] + "\n" + line + "\n" + body[nl+1:]
		} else {
			out = body + "\n" + line
		}
	} else {
		out = line + "\n" + body
	}
	return ensureSingleTrailingNewline([]byte(out))
}

// BannerInfo is a parsed banner. Fragments is the raw pin string exactly as it
// appeared after `fragments=` (empty for a v1 banner that carried no such field).
type BannerInfo struct {
	Version   string
	Src       string
	Fragments string
}

// ParseBanner reports whether content carries a generated banner at the position
// AddBanner would have written it for kind: line 1 for markdown; line 1 for shell,
// or line 2 only when line 1 is a shebang. A banner-shaped line anywhere else is
// ordinary content, not a banner — a handwritten file must never be mistaken for a
// generated one because a later line happens to match the core regex.
func ParseBanner(kind Kind, content []byte) (BannerInfo, bool) {
	lines := firstLines(content, 2)
	if len(lines) == 0 {
		return BannerInfo{}, false
	}
	if info, ok := matchBannerLine(kind, lines[0]); ok {
		return info, true
	}
	if kind == KindSH && len(lines) >= 2 && strings.HasPrefix(lines[0], "#!") {
		if info, ok := matchBannerLine(kind, lines[1]); ok {
			return info, true
		}
	}
	return BannerInfo{}, false
}

// StripBanner is the inverse of AddBanner: it removes the banner line and returns
// the body AddBanner was given, or (content, false) when no banner is present. For
// a shell artifact whose banner sits on line 2 after a shebang, the shebang is
// preserved and only the banner line is removed.
func StripBanner(kind Kind, content []byte) ([]byte, bool) {
	lines := firstLines(content, 2)
	if len(lines) == 0 {
		return content, false
	}
	if _, ok := matchBannerLine(kind, lines[0]); ok {
		return afterLine(content, 0), true
	}
	if kind == KindSH && len(lines) >= 2 && strings.HasPrefix(lines[0], "#!") {
		if _, ok := matchBannerLine(kind, lines[1]); ok {
			return append([]byte(lines[0]+"\n"), afterLine(content, 1)...), true
		}
	}
	return content, false
}

// afterLine returns the content following line index n (0-based), i.e. everything
// after the (n+1)-th newline.
func afterLine(content []byte, n int) []byte {
	rest := content
	for i := 0; i <= n; i++ {
		idx := bytes.IndexByte(rest, '\n')
		if idx < 0 {
			return nil
		}
		rest = rest[idx+1:]
	}
	return rest
}

// matchBannerLine matches the banner core within a single line AND requires the
// comment wrapper to match kind: an HTML comment for markdown, a `# ` comment for
// shell. A wrong-kind wrapper (e.g. an HTML-comment banner in a shell artifact) is
// NOT recognized, so render/check never treats it as a clobberable generated file
// — clobber-safety over recognition. This deliberately supersedes v1's kind-blind
// core matching (AddBanner always writes the matching wrapper, so no deployed
// artifact regresses).
func matchBannerLine(kind Kind, line string) (BannerInfo, bool) {
	m := bannerCoreRe.FindStringSubmatch(line)
	if m == nil {
		return BannerInfo{}, false
	}
	switch kind {
	case KindMD:
		if !strings.HasPrefix(line, "<!--") {
			return BannerInfo{}, false
		}
	case KindSH:
		if !strings.HasPrefix(line, "# ") {
			return BannerInfo{}, false
		}
	}
	return BannerInfo{Version: m[1], Src: m[2], Fragments: m[3]}, true
}

func firstLines(content []byte, n int) []string {
	var out []string
	rest := content
	for i := 0; i < n; i++ {
		idx := bytes.IndexByte(rest, '\n')
		if idx < 0 {
			if len(rest) > 0 {
				out = append(out, string(rest))
			}
			break
		}
		out = append(out, string(rest[:idx]))
		rest = rest[idx+1:]
	}
	return out
}

// ensureSingleTrailingNewline collapses trailing newlines to exactly one.
func ensureSingleTrailingNewline(b []byte) []byte {
	b = bytes.TrimRight(b, "\n")
	return append(b, '\n')
}

// EnsureSingleTrailingNewline is the exported form used by the migrator.
func EnsureSingleTrailingNewline(b []byte) []byte {
	return ensureSingleTrailingNewline(b)
}
