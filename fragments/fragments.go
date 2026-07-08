// Package fragments embeds the canonical guide bodies and serves them as a
// guide.Resolver. The six markdown guides are imported verbatim from the
// cc-skills repo-bootstrap _partials (stamp/end lines stripped); the two shell
// fragments are derived from the install-binary.sh template with the per-repo
// fills lifted into {{key}} params. Purity (LF, single trailing newline, exact
// names, markdown token-freedom) is asserted by the package tests.
package fragments

import (
	"embed"
	"path/filepath"
	"strings"

	"github.com/yasyf/cc-guides/guide"
)

//go:embed md/*.md sh/*.sh
var files embed.FS

var embedded = load()

// Resolver returns the process-wide embedded-fragment resolver.
func Resolver() guide.Resolver { return embedded }

// EmbeddedResolver serves the go:embed'd canonical fragments.
type EmbeddedResolver struct {
	byKey map[string]guide.Fragment
}

func fragKey(name string, kind guide.Kind) string {
	return name + "\x00" + kind.String()
}

func load() *EmbeddedResolver {
	r := &EmbeddedResolver{byKey: map[string]guide.Fragment{}}
	for _, dir := range []string{"md", "sh"} {
		entries, err := files.ReadDir(dir)
		if err != nil {
			panic("fragments: " + err.Error())
		}
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			kind, err := guide.KindFromExt(ext)
			if err != nil {
				panic("fragments: " + e.Name() + ": " + err.Error())
			}
			body, err := files.ReadFile(dir + "/" + e.Name())
			if err != nil {
				panic("fragments: " + err.Error())
			}
			name := strings.TrimSuffix(e.Name(), ext)
			r.byKey[fragKey(name, kind)] = guide.Fragment{
				Name:   name,
				Kind:   kind,
				Body:   body,
				Origin: "embedded",
			}
		}
	}
	return r
}

// Resolve returns the embedded fragment for name+kind, if any.
func (r *EmbeddedResolver) Resolve(name string, kind guide.Kind) (guide.Fragment, bool, error) {
	f, ok := r.byKey[fragKey(name, kind)]
	if !ok {
		return guide.Fragment{}, false, nil
	}
	return f, true, nil
}

// Entries lists every embedded fragment.
func (r *EmbeddedResolver) Entries() []guide.Entry {
	out := make([]guide.Entry, 0, len(r.byKey))
	for _, f := range r.byKey {
		out = append(out, guide.Entry{Name: f.Name, Kind: f.Kind, Origin: f.Origin})
	}
	return out
}
