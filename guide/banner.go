package guide

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// BannerCoreRegex is the stable machine contract for the generated banner. It is
// pinned here and asserted verbatim in the tests; changing it is a migration.
const BannerCoreRegex = `cc-guides (?P<version>\S+) src=(?P<src>\S+) \| GENERATED\b`

var bannerCoreRe = regexp.MustCompile(BannerCoreRegex)

// bannerCoreFmt renders the banner core. The three %s are version, src, src.
const bannerCoreFmt = "cc-guides %s src=%s | GENERATED — do not edit: change %s and run 'cc-guides render'. Everything below is in force."

// BannerCore returns the unwrapped banner core string.
func BannerCore(version, src string) string {
	return fmt.Sprintf(bannerCoreFmt, version, src, src)
}

// bannerLine wraps the core in the comment style for kind.
func bannerLine(kind Kind, version, src string) string {
	core := BannerCore(version, src)
	switch kind {
	case KindMD:
		return "<!-- " + core + " -->"
	case KindSH:
		return "# " + core
	default:
		return core
	}
}

// AddBanner splices the banner into fully-expanded content and returns bytes
// ending in exactly one newline. No blank line is inserted after the banner. For
// sh content beginning with `#!`, the shebang stays on line 1 and the banner
// becomes line 2.
func AddBanner(kind Kind, version, src string, content []byte) []byte {
	line := bannerLine(kind, version, src)
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

// BannerInfo is a parsed banner.
type BannerInfo struct {
	Version string
	Src     string
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
	if info, ok := matchBannerLine(lines[0]); ok {
		return info, true
	}
	if kind == KindSH && len(lines) >= 2 && strings.HasPrefix(lines[0], "#!") {
		if info, ok := matchBannerLine(lines[1]); ok {
			return info, true
		}
	}
	return BannerInfo{}, false
}

// matchBannerLine matches the banner core anywhere within a single line.
func matchBannerLine(line string) (BannerInfo, bool) {
	if m := bannerCoreRe.FindStringSubmatch(line); m != nil {
		return BannerInfo{Version: m[1], Src: m[2]}, true
	}
	return BannerInfo{}, false
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
