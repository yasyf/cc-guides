package guide_test

import (
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

// The banner core regex is the stable machine contract; pin it verbatim so a
// change to guide.BannerCoreRegex breaks this test loudly.
const pinnedBannerCoreRegex = `cc-guides (?P<version>\S+) src=(?P<src>\S+) \| GENERATED\b`

func TestBannerCoreRegexPinned(t *testing.T) {
	if guide.BannerCoreRegex != pinnedBannerCoreRegex {
		t.Fatalf("BannerCoreRegex drifted:\n got: %s\nwant: %s", guide.BannerCoreRegex, pinnedBannerCoreRegex)
	}
}

func TestBannerBuildParseRoundTrip(t *testing.T) {
	cases := []struct {
		kind    guide.Kind
		version string
		src     string
		content string
	}{
		{guide.KindMD, "1.2.3", "AGENTS.src.md", "# body\ncontent\n"},
		{guide.KindMD, "dev", "CLAUDE.src.md", "@AGENTS.md\n"},
		{guide.KindSH, "0.1.9", "install-binary.src.sh", "echo hi\n"},
		{guide.KindSH, "0.1.9", "install-binary.src.sh", "#!/bin/sh\necho hi\n"},
	}
	for _, tc := range cases {
		out := guide.AddBanner(tc.kind, tc.version, tc.src, []byte(tc.content))
		info, ok := guide.ParseBanner(tc.kind, out)
		if !ok {
			t.Fatalf("ParseBanner failed for %q", out)
		}
		if info.Version != tc.version {
			t.Errorf("version = %q, want %q", info.Version, tc.version)
		}
		if info.Src != tc.src {
			t.Errorf("src = %q, want %q", info.Src, tc.src)
		}
		if !strings.HasSuffix(string(out), "\n") || strings.HasSuffix(string(out), "\n\n") {
			t.Errorf("banner output must end in exactly one newline: %q", out)
		}
	}
}

func TestBannerShebangPlacement(t *testing.T) {
	out := string(guide.AddBanner(guide.KindSH, "1.0.0", "s.src.sh", []byte("#!/bin/sh\nset -e\n")))
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

	// Without a shebang the banner is line 1.
	out2 := string(guide.AddBanner(guide.KindSH, "1.0.0", "s.src.sh", []byte("set -e\n")))
	if !strings.HasPrefix(out2, "# cc-guides ") {
		t.Fatalf("non-shebang sh must start with banner: %q", out2)
	}
}

func TestParseBannerKindPosition(t *testing.T) {
	const core = "cc-guides 1.0 src=x | GENERATED"
	mdBanner := "<!-- " + core + " -->"
	shBanner := "# " + core
	cases := []struct {
		name    string
		kind    guide.Kind
		content string
		want    bool
	}{
		{"md banner on line 1", guide.KindMD, mdBanner + "\nbody\n", true},
		// A handwritten md file whose line 2 coincidentally matches the banner
		// regex must NOT be treated as generated.
		{"md prose line 1, banner line 2", guide.KindMD, "handwritten prose\n" + mdBanner + "\n", false},
		{"md banner on line 3", guide.KindMD, "one\ntwo\n" + mdBanner + "\n", false},
		{"sh banner on line 1", guide.KindSH, shBanner + "\nset -e\n", true},
		// (b) shebang line 1 + banner line 2 is where AddBanner puts it.
		{"sh shebang line 1, banner line 2", guide.KindSH, "#!/bin/sh\n" + shBanner + "\nset -e\n", true},
		// (c) non-shebang line 1 + banner line 2 is not a generated placement.
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

func TestBannerCoreExact(t *testing.T) {
	got := guide.BannerCore("1.2.3", "AGENTS.src.md")
	want := "cc-guides 1.2.3 src=AGENTS.src.md | GENERATED — do not edit: change AGENTS.src.md and run 'cc-guides render'. Everything below is in force."
	if got != want {
		t.Fatalf("core = %q\nwant %q", got, want)
	}
}
