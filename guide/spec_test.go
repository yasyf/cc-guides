package guide

import (
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
)

func TestSpecRegistryInvariants(t *testing.T) {
	if len(specs) != len(AllKinds) {
		t.Fatalf("registry has %d specs, want %d kinds", len(specs), len(AllKinds))
	}

	exts := map[string]Kind{}
	for i, s := range specs {
		kind := Kind(i)
		t.Run(s.name, func(t *testing.T) {
			if s.name == "" {
				t.Fatalf("kind %d has an empty name", kind)
			}
			if len(s.exts) == 0 {
				t.Fatalf("kind %s has no extensions", kind)
			}

			switch s.category {
			case catMerge:
				if s.codec == nil {
					t.Fatalf("kind %s is catMerge but has no codec", kind)
				}
				if s.opener != nil {
					t.Fatalf("kind %s is catMerge but has an opener", kind)
				}
				if s.shebang {
					t.Fatalf("kind %s is catMerge but permits a shebang", kind)
				}
			case catAppend:
				if s.codec != nil {
					t.Fatalf("kind %s is catAppend but has a codec", kind)
				}
			default:
				t.Fatalf("kind %s has unknown category %d", kind, s.category)
			}

			if s.opener != nil {
				if s.grammar == nil {
					t.Fatalf("kind %s has an opener but no grammar", kind)
				}
				language := ts.NewLanguage(s.grammar.lang)
				for _, name := range s.opener.kinds {
					if language.IdForNodeKind(name, true) == 0 {
						t.Fatalf("kind %s opener node kind %q is absent from its grammar", kind, name)
					}
				}
			}

			for _, ext := range s.exts {
				if other, ok := exts[ext]; ok {
					t.Fatalf("kind %s extension %q duplicates kind %s", kind, ext, other)
				}
				exts[ext] = kind
			}
		})
	}
}
