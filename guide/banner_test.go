package guide_test

import (
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

// The banner core regex is the stable machine contract; pin it verbatim so a
// change to guide.BannerCoreRegex breaks this test loudly. The optional
// `fragments=` group is what makes ONE regex parse both v1 and v2 banners.
const pinnedBannerCoreRegex = `cc-guides (?P<version>\S+) src=(?P<src>\S+)(?: fragments=(?P<fragments>\S+))? \| GENERATED\b`

func TestBannerCoreRegexPinned(t *testing.T) {
	if guide.BannerCoreRegex != pinnedBannerCoreRegex {
		t.Fatalf("BannerCoreRegex drifted:\n got: %s\nwant: %s", guide.BannerCoreRegex, pinnedBannerCoreRegex)
	}
}

func TestBannerBuildParseRoundTrip(t *testing.T) {
	cases := []struct {
		kind      guide.Kind
		version   string
		src       string
		fragments string
		content   string
	}{
		{guide.KindMD, "1.2.3", ".claude/fragments/AGENTS.md", "cc-skills@abcdef012345", "# body\ncontent\n"},
		{guide.KindMD, "dev", ".claude/fragments/CLAUDE.md", "none", "@AGENTS.md\n"},
		{guide.KindMD, "0.2.0", ".claude/fragments/AGENTS.md", "cc-skills@abcdef012345,team@0123456789ab", "x\n"},
		{guide.KindSH, "0.1.9", ".claude/fragments/plugin/scripts/install.sh", "local", "echo hi\n"},
		{guide.KindSH, "0.1.9", ".claude/fragments/plugin/scripts/install.sh", "cc-skills@abcdef012345", "#!/bin/sh\necho hi\n"},
	}
	for _, tc := range cases {
		out := guide.AddBanner(tc.kind, tc.version, tc.src, tc.fragments, []byte(tc.content))
		info, ok := guide.ParseBanner(tc.kind, out)
		if !ok {
			t.Fatalf("ParseBanner failed for %q", out)
		}
		if info.Version != tc.version || info.Src != tc.src || info.Fragments != tc.fragments {
			t.Errorf("parsed %+v, want version=%q src=%q fragments=%q", info, tc.version, tc.src, tc.fragments)
		}
		if !strings.HasSuffix(string(out), "\n") || strings.HasSuffix(string(out), "\n\n") {
			t.Errorf("banner output must end in exactly one newline: %q", out)
		}
	}
}

// A deployed v1 banner (no fragments= field) must still parse, with an empty
// Fragments — the whole point of the optional group.
func TestParseV1Banner(t *testing.T) {
	v1 := "<!-- cc-guides 0.1.7 src=AGENTS.src.md | GENERATED — do not edit -->\n# body\n"
	info, ok := guide.ParseBanner(guide.KindMD, []byte(v1))
	if !ok {
		t.Fatal("v1 banner must parse")
	}
	if info.Version != "0.1.7" || info.Src != "AGENTS.src.md" || info.Fragments != "" {
		t.Fatalf("v1 parse = %+v", info)
	}
}

func TestBannerShebangPlacement(t *testing.T) {
	out := string(guide.AddBanner(guide.KindSH, "1.0.0", "s.sh", "none", []byte("#!/bin/sh\nset -e\n")))
	lines := strings.Split(out, "\n")
	if lines[0] != "#!/bin/sh" {
		t.Fatalf("line 1 = %q, want shebang", lines[0])
	}
	if !strings.HasPrefix(lines[1], "# cc-guides ") {
		t.Fatalf("line 2 = %q, want banner", lines[1])
	}
	if lines[2] != "set -e" {
		t.Fatalf("line 3 = %q, want set -e", lines[2])
	}
	out2 := string(guide.AddBanner(guide.KindSH, "1.0.0", "s.sh", "none", []byte("set -e\n")))
	if !strings.HasPrefix(out2, "# cc-guides ") {
		t.Fatalf("non-shebang sh must start with banner: %q", out2)
	}
}

// StripBanner is the exact inverse of AddBanner (round-trip), including the
// shebang-preserving shell case.
func TestStripBannerRoundTrip(t *testing.T) {
	cases := []struct {
		kind guide.Kind
		body string
	}{
		{guide.KindMD, "# head\n\nbody\n"},
		{guide.KindSH, "echo hi\n"},
		{guide.KindSH, "#!/bin/sh\nset -e\necho hi\n"},
	}
	for _, tc := range cases {
		final := guide.AddBanner(tc.kind, "1.0.0", "x", "none", []byte(tc.body))
		got, ok := guide.StripBanner(tc.kind, final)
		if !ok {
			t.Fatalf("StripBanner failed for %q", final)
		}
		if string(got) != tc.body {
			t.Fatalf("round-trip mismatch:\n got %q\nwant %q", got, tc.body)
		}
	}
	if _, ok := guide.StripBanner(guide.KindMD, []byte("no banner here\n")); ok {
		t.Fatal("StripBanner must report false on a bannerless file")
	}
}

func TestParseBannerKindPosition(t *testing.T) {
	const core = "cc-guides 1.0 src=x fragments=none | GENERATED"
	mdBanner := "<!-- " + core + " -->"
	shBanner := "# " + core
	cases := []struct {
		name    string
		kind    guide.Kind
		content string
		want    bool
	}{
		{"md banner on line 1", guide.KindMD, mdBanner + "\nbody\n", true},
		{"md prose line 1, banner line 2", guide.KindMD, "handwritten prose\n" + mdBanner + "\n", false},
		{"md banner on line 3", guide.KindMD, "one\ntwo\n" + mdBanner + "\n", false},
		{"sh banner on line 1", guide.KindSH, shBanner + "\nset -e\n", true},
		{"sh shebang line 1, banner line 2", guide.KindSH, "#!/bin/sh\n" + shBanner + "\nset -e\n", true},
		{"sh non-shebang line 1, banner line 2", guide.KindSH, "echo hi\n" + shBanner + "\n", false},
		{"sh banner on line 3 with shebang", guide.KindSH, "#!/bin/sh\nset -e\n" + shBanner + "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := guide.ParseBanner(tc.kind, []byte(tc.content)); ok != tc.want {
				t.Fatalf("ParseBanner ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

// ParseBanner is kind-AWARE about the comment wrapper: a banner whose wrapper does
// not match the artifact kind is NOT recognized, so render/check never treats a
// wrong-kind file as a clobberable generated artifact. This deliberately supersedes
// v1's kind-blind core matching (clobber-safety over recognition; AddBanner always
// writes the matching wrapper, so no deployed artifact regresses).
func TestParseBannerKindAwareCommentStyle(t *testing.T) {
	const core = "cc-guides 0.1.7 src=AGENTS.src.md | GENERATED"
	// Wrong-kind wrappers are rejected in BOTH directions.
	if _, ok := guide.ParseBanner(guide.KindSH, []byte("<!-- "+core+" -->\nset -e\n")); ok {
		t.Fatal("md-wrapped banner must NOT parse under KindSH")
	}
	if _, ok := guide.ParseBanner(guide.KindMD, []byte("# "+core+"\nbody\n")); ok {
		t.Fatal("sh-wrapped banner must NOT parse under KindMD")
	}
	// Matching wrappers still parse.
	if _, ok := guide.ParseBanner(guide.KindMD, []byte("<!-- "+core+" -->\nbody\n")); !ok {
		t.Fatal("md-wrapped banner must parse under KindMD")
	}
	if _, ok := guide.ParseBanner(guide.KindSH, []byte("# "+core+"\nset -e\n")); !ok {
		t.Fatal("sh-wrapped banner must parse under KindSH")
	}
}

func TestBannerCoreExact(t *testing.T) {
	got := guide.BannerCore("1.2.3", ".claude/fragments/AGENTS.md", "cc-skills@abcdef012345")
	want := "cc-guides 1.2.3 src=.claude/fragments/AGENTS.md fragments=cc-skills@abcdef012345 | GENERATED — do not edit: edit .claude/fragments/AGENTS.md/ and run 'cc-guides render'. Everything below is in force."
	if got != want {
		t.Fatalf("core = %q\nwant %q", got, want)
	}
}

// The marker core regex is the stable machine contract; pin it verbatim.
const pinnedMarkerCoreRegex = `GENERATED by cc-guides from (?P<src>\S+)/ — do not edit; edit the fragments and run 'cc-guides render'\.`

func TestMarkerCoreRegexPinned(t *testing.T) {
	if guide.MarkerCoreRegex != pinnedMarkerCoreRegex {
		t.Fatalf("MarkerCoreRegex drifted:\n got: %s\nwant: %s", guide.MarkerCoreRegex, pinnedMarkerCoreRegex)
	}
}

func TestMarkerCoreExact(t *testing.T) {
	got := guide.MarkerLine(guide.KindMD, ".claude/fragments/AGENTS.md")
	want := "<!-- GENERATED by cc-guides from .claude/fragments/AGENTS.md/ — do not edit; edit the fragments and run 'cc-guides render'. -->"
	if got != want {
		t.Fatalf("md marker = %q\nwant %q", got, want)
	}
	if sh := guide.MarkerLine(guide.KindSH, "x"); sh != "# GENERATED by cc-guides from x/ — do not edit; edit the fragments and run 'cc-guides render'." {
		t.Fatalf("sh marker = %q", sh)
	}
}

// AddMarker / ParseMarker / StripMarker round-trip, including the shell
// shebang-preserving case, and the src round-trips.
func TestMarkerAddParseStripRoundTrip(t *testing.T) {
	cases := []struct {
		kind guide.Kind
		src  string
		body string
	}{
		{guide.KindMD, ".claude/fragments/AGENTS.md", "# head\n\nbody\n"},
		{guide.KindSH, ".claude/fragments/plugin/scripts/install.sh", "echo hi\n"},
		{guide.KindSH, ".claude/fragments/plugin/scripts/install.sh", "#!/bin/sh\nset -e\necho hi\n"},
	}
	for _, tc := range cases {
		final := guide.AddMarker(tc.kind, tc.src, []byte(tc.body))
		if !strings.HasSuffix(string(final), "\n") || strings.HasSuffix(string(final), "\n\n") {
			t.Errorf("marker output must end in exactly one newline: %q", final)
		}
		info, ok := guide.ParseMarker(tc.kind, final)
		if !ok || info.Src != tc.src {
			t.Fatalf("ParseMarker = %+v ok=%v, want src=%q", info, ok, tc.src)
		}
		got, ok := guide.StripMarker(tc.kind, final)
		if !ok || string(got) != tc.body {
			t.Fatalf("StripMarker round-trip:\n got %q\nwant %q", got, tc.body)
		}
	}
	if _, ok := guide.StripMarker(guide.KindMD, []byte("no marker here\n")); ok {
		t.Fatal("StripMarker must report false on a markerless file")
	}
}

func TestMarkerShebangPlacement(t *testing.T) {
	out := string(guide.AddMarker(guide.KindSH, "s.sh", []byte("#!/bin/sh\nset -e\n")))
	lines := strings.Split(out, "\n")
	if lines[0] != "#!/bin/sh" {
		t.Fatalf("line 1 = %q, want shebang", lines[0])
	}
	if !strings.HasPrefix(lines[1], "# GENERATED by cc-guides from s.sh/") {
		t.Fatalf("line 2 = %q, want marker", lines[1])
	}
}

// ParseMarker is kind-aware about the wrapper and position, exactly like ParseBanner.
func TestParseMarkerKindPosition(t *testing.T) {
	mdMarker := guide.MarkerLine(guide.KindMD, "x")
	shMarker := guide.MarkerLine(guide.KindSH, "x")
	cases := []struct {
		name    string
		kind    guide.Kind
		content string
		want    bool
	}{
		{"md marker line 1", guide.KindMD, mdMarker + "\nbody\n", true},
		{"md marker line 3", guide.KindMD, "one\ntwo\n" + mdMarker + "\n", false},
		{"wrong-kind wrapper", guide.KindSH, mdMarker + "\nset -e\n", false},
		{"sh shebang then marker", guide.KindSH, "#!/bin/sh\n" + shMarker + "\nset -e\n", true},
		{"sh non-shebang line 1", guide.KindSH, "echo hi\n" + shMarker + "\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := guide.ParseMarker(tc.kind, []byte(tc.content)); ok != tc.want {
				t.Fatalf("ParseMarker ok = %v, want %v", ok, tc.want)
			}
		})
	}
}
