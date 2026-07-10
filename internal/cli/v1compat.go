package cli

import (
	"context"
	"path/filepath"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/migrate"
	"github.com/yasyf/cc-guides/source"
)

// mapResolver is a fixed in-memory guide.Resolver keyed by name+kind. It carries
// no context — the transitional v1 renderer pre-resolves remote fragments into one
// so guide.Render (which has no ctx parameter) never touches the network.
type mapResolver map[string]guide.Fragment

func (m mapResolver) Resolve(name string, kind guide.Kind) (guide.Fragment, bool, error) {
	f, ok := m[name+"|"+kind.String()]
	return f, ok, nil
}

// srcAdapter presents a source.Importer as a guide.Resolver for one alias. init's
// stamp scan discovers fragment names on the fly, so — unlike render — it cannot
// pre-resolve and must resolve lazily; the captured ctx is the command's own
// request context.
type srcAdapter struct {
	ctx   context.Context
	imp   source.Importer
	alias string
}

//nolint:contextcheck // guide.Resolver has no ctx parameter; a.ctx is the command's inherited request context
func (a srcAdapter) Resolve(name string, kind guide.Kind) (guide.Fragment, bool, error) {
	body, found, err := a.imp.Resolve(a.ctx, a.alias, name, kind)
	if err != nil || !found {
		return guide.Fragment{}, found, err
	}
	return guide.Fragment{Name: name, Kind: kind, Body: body, Origin: a.alias + ":" + name}, true, nil
}

// newV1Resolver builds the source resolver for the transitional path: the baked
// cc-skills default plus any --source overrides, with pinned shas (check) or fresh
// resolution (render).
func newV1Resolver(overrides, pinned map[string]string) (*source.Resolver, error) {
	specs := map[string]string{migrate.CCSkillsAlias: migrate.CCSkillsSpec}
	for alias, spec := range overrides {
		specs[alias] = spec
	}
	return source.New(source.Options{Specs: specs, Pinned: pinned})
}

// v1Chain pre-resolves every remote fragment a v1 doc references (using ctx here,
// never capturing it) and returns a resolver that chains local flat overrides —
// which v1 repos may still carry — over those fetched bodies.
func v1Chain(ctx context.Context, root string, doc *guide.Doc, kind guide.Kind, resolver *source.Resolver) (guide.Resolver, error) {
	remote := mapResolver{}
	for _, n := range doc.Nodes {
		if n.Include == nil {
			continue
		}
		key := n.Include.Name + "|" + kind.String()
		if _, done := remote[key]; done {
			continue
		}
		body, found, err := resolver.Resolve(ctx, migrate.CCSkillsAlias, n.Include.Name, kind)
		if err != nil {
			return nil, err
		}
		if found {
			remote[key] = guide.Fragment{Name: n.Include.Name, Kind: kind, Body: body, Origin: migrate.CCSkillsAlias + ":" + n.Include.Name}
		}
	}
	overrideDir := filepath.Join(root, filepath.FromSlash(guide.FragmentsRoot))
	return guide.NewChain(guide.NewDirResolver(overrideDir, guide.FragmentsRoot), remote), nil
}
