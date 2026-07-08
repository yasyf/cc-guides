// Package guide is a generic, kind-aware renderer for source files that expand
// column-0 `{{> name}}` include directives against a Resolver and carry a stable
// GENERATED banner. It knows nothing about the specific fragments it renders:
// callers supply a Resolver (see the fragments package for the embedded canonical
// bodies, and DirResolver/Chain for local overrides). The pipeline is Parse → a
// Doc of literal and include Nodes → Render → AddBanner, with a sentinel error
// taxonomy the CLI maps to exit codes.
package guide
