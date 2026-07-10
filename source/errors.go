package source

import "errors"

// Sentinel errors. All denote invalid input or an unreachable source (CLI exit 2).
var (
	// ErrBadSpec is a malformed source spec.
	ErrBadSpec = errors.New("invalid source spec")
	// ErrUnknownAlias is an import whose alias no source declares.
	ErrUnknownAlias = errors.New("unknown source alias")
	// ErrResolveRef is a failure to resolve a ref to a commit sha via git ls-remote.
	ErrResolveRef = errors.New("could not resolve source ref")
	// ErrFetch is a failure to fetch or extract a source tarball.
	ErrFetch = errors.New("could not fetch source content")
	// ErrOfflineMiss is a cache miss with no network reachability.
	ErrOfflineMiss = errors.New("source content is not cached and could not be fetched")
	// ErrBadManifest is a malformed or invalid cc-guides.toml pack manifest.
	ErrBadManifest = errors.New("invalid cc-guides.toml manifest")
	// ErrNoManifest is a manifest-form spec whose target repo carries no
	// cc-guides.toml (neither .claude/ nor root).
	ErrNoManifest = errors.New("no cc-guides.toml manifest in source repo")
	// ErrManifestGuides is a manifest whose guides dir is missing in the tree.
	ErrManifestGuides = errors.New("manifest guides dir not found in source repo")
)
