package guide

import "sort"

// Fragment is a resolved fragment body plus its provenance.
type Fragment struct {
	Name   string
	Kind   Kind
	Body   []byte
	Origin string // "embedded" or a repo-relative override path
	Local  bool   // true when resolved from an override dir (renders with local: markers)
}

// Entry describes a fragment a resolver can serve, for `list`.
type Entry struct {
	Name   string
	Kind   Kind
	Origin string
	Local  bool
}

// Resolver resolves a fragment name+kind to its body.
type Resolver interface {
	Resolve(name string, kind Kind) (Fragment, bool, error)
}

// Lister optionally enumerates a resolver's fragments.
type Lister interface {
	Entries() []Entry
}

// Chain resolves against an ordered list of resolvers: the first hit wins, so
// earlier resolvers (overrides) shadow later ones (embedded).
type Chain struct {
	resolvers []Resolver
}

// NewChain builds a Chain over the given resolvers, highest-priority first.
func NewChain(resolvers ...Resolver) *Chain {
	return &Chain{resolvers: resolvers}
}

// Resolve returns the first resolver's hit for name+kind.
func (c *Chain) Resolve(name string, kind Kind) (Fragment, bool, error) {
	for _, r := range c.resolvers {
		f, ok, err := r.Resolve(name, kind)
		if err != nil {
			return Fragment{}, false, err
		}
		if ok {
			return f, true, nil
		}
	}
	return Fragment{}, false, nil
}

// Entries merges every underlying Lister, shadowing by name+kind (earlier wins),
// and returns them sorted by name then kind.
func (c *Chain) Entries() []Entry {
	seen := map[string]bool{}
	var out []Entry
	for _, r := range c.resolvers {
		l, ok := r.(Lister)
		if !ok {
			continue
		}
		for _, e := range l.Entries() {
			key := e.Name + "\x00" + e.Kind.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Kind.String() < out[j].Kind.String()
	})
	return out
}
